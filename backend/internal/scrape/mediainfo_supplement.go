package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
	"qmediasync/internal/v115open"
)

// 补充媒体信息（-mediainfo.json）的独立模块。
//
// 与正常刮削完全解耦：不识别、不查 TMDB、不写 NFO、不下载图片、不重命名、
// 不改 scrape_media 的状态，只做「取直链 → ffprobe → 生成 json → 交给上传队列」。
//
// 之所以需要它：FFprobeJson 是内存字段不落库，且 shouldSkipFFprobe 在已有
// 编码缓存时直接跳过探测，所以老记录即便重跑刮削也不会生成 mediainfo.json；
// 唯一的替代方案是「重新刮削」，但会重写 NFO/图片并可能重新命名。
//
// 风控复用：
//   - 115 取直链走 v115open 的 QPS/QPM/QPH 队列
//   - 115 拉流走内置 /proxy-115 反代（固定 UA、下载域名白名单）
//   - 上传走 DbUploadTask 统一队列（去重、重试、全局并发）
//   - 探测并发由本模块自己的 Concurrency 控制，默认 1

const (
	mediaInfoSupplementDefaultConcurrency = 1
	mediaInfoSupplementMaxConcurrency     = 3
	mediaInfoSupplementMaxErrors          = 30
	mediaInfoSupplementListPageSize       = 200
	mediaInfoSupplementMaxListPages       = 5
)

// MediaInfoSupplementOptions 补充任务选项。
type MediaInfoSupplementOptions struct {
	Overwrite    bool `json:"overwrite"`     // 目标目录已有同名 json 时是否覆盖
	Concurrency  int  `json:"concurrency"`   // 探测并发，1-3
	IncludeMovie bool `json:"include_movie"` // 是否处理电影
	IncludeTV    bool `json:"include_tv"`    // 是否处理剧集
}

func (o *MediaInfoSupplementOptions) normalize() {
	if o.Concurrency <= 0 {
		o.Concurrency = mediaInfoSupplementDefaultConcurrency
	}
	if o.Concurrency > mediaInfoSupplementMaxConcurrency {
		o.Concurrency = mediaInfoSupplementMaxConcurrency
	}
	if !o.IncludeMovie && !o.IncludeTV {
		o.IncludeMovie = true
		o.IncludeTV = true
	}
}

// MediaInfoSupplementProgress 任务进度，供前端轮询。
type MediaInfoSupplementProgress struct {
	ScrapePathID uint     `json:"scrape_path_id"`
	Running      bool     `json:"running"`
	Total        int      `json:"total"`
	Done         int      `json:"done"`
	Success      int      `json:"success"`
	Skipped      int      `json:"skipped"`
	Failed       int      `json:"failed"`
	Current      string   `json:"current"`
	StartedAt    int64    `json:"started_at"`
	FinishedAt   int64    `json:"finished_at"`
	Errors       []string `json:"errors"`
}

// MediaInfoSupplement 一次补充任务。
type MediaInfoSupplement struct {
	scrapePath *models.ScrapePath
	base       *ScrapeBase

	options MediaInfoSupplementOptions
	files   []*models.ScrapeMediaFile

	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	running    bool
	done       int
	success    int
	skipped    int
	failed     int
	current    string
	startedAt  int64
	finishedAt int64
	errors     []string
}

var (
	mediaInfoSupplementTasksMu sync.Mutex
	mediaInfoSupplementTasks   = make(map[uint]*MediaInfoSupplement)
)

// StartMediaInfoSupplement 启动一次补充任务。
//
// 只处理该刮削路径下「已刮削 / 已整理」且有 pickcode 的记录；没有可处理记录时
// 直接返回错误，避免前端点了按钮却看不到任何反馈。
func StartMediaInfoSupplement(sp *models.ScrapePath, opts MediaInfoSupplementOptions) (*MediaInfoSupplement, error) {
	if sp == nil {
		return nil, fmt.Errorf("刮削目录不存在")
	}
	if sp.IsRunning() {
		return nil, fmt.Errorf("该刮削目录正在执行刮削任务，请等刮削结束后再补充媒体信息")
	}
	opts.normalize()

	mediaInfoSupplementTasksMu.Lock()
	if exist, ok := mediaInfoSupplementTasks[sp.ID]; ok && exist.isRunning() {
		mediaInfoSupplementTasksMu.Unlock()
		return nil, fmt.Errorf("该刮削目录已有补充任务在执行")
	}
	mediaInfoSupplementTasksMu.Unlock()

	files := collectSupplementFiles(sp, opts)
	if len(files) == 0 {
		return nil, fmt.Errorf("没有可补充的记录：该目录下没有带直链信息的视频，请先扫描一次该目录")
	}

	// 复用刮削的客户端初始化（115/OpenList/百度），保证取直链与在线播放同一条链路。
	sc := NewScrape(sp)
	if err := sc.initOpenClient(); err != nil {
		return nil, fmt.Errorf("初始化网盘客户端失败：%w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &MediaInfoSupplement{
		scrapePath: sp,
		base: &ScrapeBase{
			scrapePath:     sp,
			ctx:            ctx,
			v115Client:     sc.V115Client,
			openlistClient: sc.OpenlistClient,
			baiduPanClient: sc.BaiduPanClient,
		},
		options:   opts,
		files:     files,
		ctx:       ctx,
		cancel:    cancel,
		running:   true,
		startedAt: time.Now().Unix(),
	}
	if err := os.MkdirAll(t.tmpDir(), 0777); err != nil {
		cancel()
		return nil, fmt.Errorf("创建临时目录失败：%w", err)
	}

	mediaInfoSupplementTasksMu.Lock()
	mediaInfoSupplementTasks[sp.ID] = t
	mediaInfoSupplementTasksMu.Unlock()

	helpers.AppLogger.Infof("[媒体信息补充] 目录 #%d %s 开始，待处理 %d 条，并发 %d，覆盖已有：%v",
		sp.ID, sp.SourcePath, len(files), opts.Concurrency, opts.Overwrite)
	go t.Run()
	return t, nil
}

// GetMediaInfoSupplementProgress 查询进度；没有任务时返回 running=false 的空结构。
func GetMediaInfoSupplementProgress(scrapePathID uint) MediaInfoSupplementProgress {
	mediaInfoSupplementTasksMu.Lock()
	t, ok := mediaInfoSupplementTasks[scrapePathID]
	mediaInfoSupplementTasksMu.Unlock()
	if !ok {
		return MediaInfoSupplementProgress{ScrapePathID: scrapePathID}
	}
	return t.progress()
}

// StopMediaInfoSupplement 停止任务，返回是否确实停了一个正在运行的任务。
func StopMediaInfoSupplement(scrapePathID uint) bool {
	mediaInfoSupplementTasksMu.Lock()
	t, ok := mediaInfoSupplementTasks[scrapePathID]
	mediaInfoSupplementTasksMu.Unlock()
	if !ok || !t.isRunning() {
		return false
	}
	t.cancel()
	helpers.AppLogger.Infof("[媒体信息补充] 目录 #%d 收到停止信号", scrapePathID)
	return true
}

func collectSupplementFiles(sp *models.ScrapePath, opts MediaInfoSupplementOptions) []*models.ScrapeMediaFile {
	files := make([]*models.ScrapeMediaFile, 0)
	if opts.IncludeMovie {
		files = append(files, models.GetScrapedMediaFilesForMediaInfoSupplement(sp.ID, models.MediaTypeMovie)...)
	}
	if opts.IncludeTV {
		files = append(files, models.GetScrapedMediaFilesForMediaInfoSupplement(sp.ID, models.MediaTypeTvShow)...)
	}
	return files
}

// Run 执行任务，直到处理完毕或被停止。
func (t *MediaInfoSupplement) Run() {
	defer func() {
		t.mu.Lock()
		t.running = false
		t.current = ""
		t.finishedAt = time.Now().Unix()
		success, skipped, failed := t.success, t.skipped, t.failed
		t.mu.Unlock()
		helpers.AppLogger.Infof("[媒体信息补充] 目录 #%d 结束：成功 %d，跳过 %d，失败 %d",
			t.scrapePath.ID, success, skipped, failed)
	}()

	ch := make(chan *models.ScrapeMediaFile)
	var wg sync.WaitGroup
	for i := 0; i < t.options.Concurrency; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for {
				select {
				case <-t.ctx.Done():
					return
				case file, ok := <-ch:
					if !ok {
						return
					}
					t.processOne(file)
				}
			}
		}(i + 1)
	}

feed:
	for _, file := range t.files {
		select {
		case <-t.ctx.Done():
			break feed
		case ch <- file:
		}
	}
	close(ch)
	wg.Wait()
}

func (t *MediaInfoSupplement) processOne(file *models.ScrapeMediaFile) {
	defer func() {
		if r := recover(); r != nil {
			t.recordFailure(file, fmt.Errorf("处理时发生 panic：%v", r))
		}
		t.mu.Lock()
		t.done++
		t.current = ""
		t.mu.Unlock()
	}()

	t.mu.Lock()
	t.current = file.VideoFilename
	t.mu.Unlock()

	fileName := mediaInfoFileName(file)
	if fileName == "" {
		t.recordFailure(file, fmt.Errorf("无法确定视频文件名"))
		return
	}
	destPath, destPathID := supplementDest(file)
	if destPath == "" || destPathID == "" {
		t.recordFailure(file, fmt.Errorf("缺少目标目录信息（NewPathId/NewSeasonPathId 为空）"))
		return
	}
	// 未整理的剧集记录里只有剧集根目录，视频通常在 Season xx 子目录下；
	// json 必须与视频同目录才能被客户端读到，所以按文件名再定位一次。
	if file.MediaType == models.MediaTypeTvShow && !hasDestTarget(file) {
		destPath, destPathID = t.locateVideoDir(destPath, destPathID, filepath.Base(file.VideoFilename))
	}

	if !t.options.Overwrite {
		exists, err := t.alreadyExists(file, destPath, destPathID, fileName)
		if err != nil {
			helpers.AppLogger.Warnf("[媒体信息补充] 检查 %s 是否已存在失败，将直接覆盖：%v", fileName, err)
		} else if exists {
			t.recordSkip()
			return
		}
	}

	ff, err := t.probe(file)
	if err != nil {
		t.recordFailure(file, err)
		return
	}
	raw, err := json.Marshal(ff)
	if err != nil {
		t.recordFailure(file, fmt.Errorf("序列化 ffprobe 数据失败：%w", err))
		return
	}
	content, err := helpers.BuildMediaInfoJSON(raw)
	if err != nil {
		t.recordFailure(file, fmt.Errorf("生成媒体信息失败：%w", err))
		return
	}
	localPath := filepath.Join(t.tmpDir(), fmt.Sprintf("%d-%s", file.ID, fileName))
	if err := os.WriteFile(localPath, content, 0644); err != nil {
		t.recordFailure(file, fmt.Errorf("写入临时文件失败：%w", err))
		return
	}
	// 走 AV 上传通道（而不是刮削通道）：刮削通道在收尾时会 RemoveTmpFiles，
	// 进而触发“来源目录是否为空”的判断；补充任务不应该碰刮削临时目录和来源目录。
	// AV 通道上传完成后只删除这一次生成的本地 json，符合“只补文件”的语义。
	if err := models.AddUploadTaskFromAV(
		t.scrapePath.AccountId, t.scrapePath.SourceType, fileName, localPath,
		filepath.Join(destPath, fileName), destPathID,
	); err != nil {
		t.recordFailure(file, fmt.Errorf("加入上传队列失败：%w", err))
		return
	}
	t.mu.Lock()
	t.success++
	t.mu.Unlock()
	helpers.AppLogger.Infof("[媒体信息补充] 已入队上传：%s", filepath.Join(destPath, fileName))
}

// probe 取直链并执行 ffprobe，返回原始探测数据。
func (t *MediaInfoSupplement) probe(file *models.ScrapeMediaFile) (*helpers.FFprobeJson, error) {
	var target string
	switch file.SourceType {
	case models.SourceTypeLocal:
		// 本地类型 VideoPickCode 存的是绝对路径
		target = file.VideoPickCode
	case models.SourceType115:
		if t.base.v115Client == nil {
			return nil, fmt.Errorf("115 客户端未初始化")
		}
		direct := t.base.v115Client.GetDownloadUrl(context.Background(), file.VideoPickCode, v115open.DEFAULTUA, false)
		if direct == "" {
			return nil, fmt.Errorf("获取 115 下载直链失败（pickcode 可能已失效，建议重新扫描该目录）")
		}
		// 与刮削/播放一致：走内置反代，UA 与域名白名单由 QMS 统一管理
		target = fmt.Sprintf("http://127.0.0.1:12333/proxy-115?url=%s", url.QueryEscape(direct))
	case models.SourceTypeOpenList:
		if t.base.openlistClient == nil {
			return nil, fmt.Errorf("OpenList 客户端未初始化")
		}
		target = t.base.openlistClient.GetRawUrl(file.VideoPickCode)
		if target == "" {
			return nil, fmt.Errorf("获取 OpenList 直链失败")
		}
	default:
		return nil, fmt.Errorf("该网盘类型（%s）暂不支持补充媒体信息", file.SourceType)
	}

	ff, err := helpers.GetFFprobeJson(target)
	if err != nil {
		return nil, fmt.Errorf("ffprobe 失败：%w", err)
	}
	if ff == nil || len(ff.Streams) == 0 {
		return nil, fmt.Errorf("ffprobe 未返回任何流信息")
	}
	return ff, nil
}

// alreadyExists 判断目标目录下是否已有同名 json。
// 只有本地与 115 能低成本判断；其他来源返回 false（按覆盖处理）。
func (t *MediaInfoSupplement) alreadyExists(file *models.ScrapeMediaFile, destPath, destPathID, fileName string) (bool, error) {
	switch file.SourceType {
	case models.SourceTypeLocal:
		return helpers.PathExists(filepath.Join(destPath, fileName)), nil
	case models.SourceType115:
		if t.base.v115Client == nil {
			return false, fmt.Errorf("115 客户端未初始化")
		}
		offset := 0
		for page := 0; page < mediaInfoSupplementMaxListPages; page++ {
			resp, err := t.base.v115Client.GetFsList(t.ctx, destPathID, true, false, true, offset, mediaInfoSupplementListPageSize)
			if err != nil {
				return false, err
			}
			if resp == nil {
				return false, nil
			}
			for _, f := range resp.Data {
				if f.FileName == fileName {
					return true, nil
				}
			}
			if len(resp.Data) < mediaInfoSupplementListPageSize {
				return false, nil
			}
			offset += mediaInfoSupplementListPageSize
		}
		return false, nil
	default:
		return false, nil
	}
}

func mediaInfoFileName(file *models.ScrapeMediaFile) string {
	base := strings.TrimSpace(file.NewVideoBaseName)
	if base == "" {
		name := filepath.Base(file.VideoFilename)
		base = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if base == "" || base == "." {
		return ""
	}
	return base + "-mediainfo.json"
}

// supplementDest 返回 json 的目标目录与目录 ID。
//
// 优先整理后的目录（已刮削/已整理的条目有 NewPathName 与对应 PathId）；
// 缺失时退回视频当前所在目录，这样未识别、识别未刮削、刮削失败、重命名失败的
// 条目也能把 json 补在视频旁边，而不是直接跳过。
func supplementDest(file *models.ScrapeMediaFile) (string, string) {
	if file.MediaType == models.MediaTypeTvShow {
		if hasDestTarget(file) {
			pathID := file.NewSeasonPathId
			if pathID == "" {
				pathID = file.NewPathId
			}
			return file.GetDestFullSeasonPath(), pathID
		}
		return sourceDir(file)
	}
	if hasDestTarget(file) {
		return file.GetDestFullMoviePath(), file.NewPathId
	}
	return sourceDir(file)
}

// hasDestTarget 判断记录是否有可用的「整理后目录」。
func hasDestTarget(file *models.ScrapeMediaFile) bool {
	if file.NewPathName == "" {
		return false
	}
	if file.MediaType == models.MediaTypeTvShow {
		return file.NewSeasonPathId != "" || file.NewPathId != ""
	}
	return file.NewPathId != ""
}

// sourceDir 视频当前所在目录。
// 本地类型的 VideoPickCode 存的是视频绝对路径，直接取父目录；
// 网盘类型用记录里的目录路径与目录 ID（PathId 即视频所在目录的 ID）。
// 剧集扫描时 Path 通常为空，退回剧集目录；真正的季子目录由 locateVideoDir 再定位。
func sourceDir(file *models.ScrapeMediaFile) (string, string) {
	if file.SourceType == models.SourceTypeLocal && file.VideoPickCode != "" {
		dir := filepath.Dir(file.VideoPickCode)
		return dir, dir
	}
	dir, dirID := file.Path, file.PathId
	if dir == "" {
		dir, dirID = file.TvshowPath, file.TvshowPathId
	}
	if dir == "" {
		dir, dirID = file.SourcePath, file.SourcePathId
	}
	if file.SourcePath != "" && dir != "" && !strings.HasPrefix(dir, file.SourcePath) {
		dir = filepath.Join(file.SourcePath, dir)
	}
	return dir, dirID
}

// locateVideoDir 在剧集根目录下按视频文件名定位真正所在目录（最多往下两层，
// 通常是 Season xx）。未整理的剧集记录里只有剧集根目录，json 必须与视频同目录
// 才能被客户端读到，所以这里做一次轻量列目录。
//
// 仅 115 支持列目录；其他来源或定位失败时原样返回根目录。
func (t *MediaInfoSupplement) locateVideoDir(rootPath, rootID, videoName string) (string, string) {
	if t.base.v115Client == nil || rootID == "" || videoName == "" {
		return rootPath, rootID
	}
	files, err := t.listDir(rootID)
	if err != nil {
		helpers.AppLogger.Warnf("[媒体信息补充] 列出目录 %s 失败，按剧集根目录处理：%v", rootPath, err)
		return rootPath, rootID
	}
	subDirs := make([]v115open.File, 0, 4)
	for _, f := range files {
		if f.FileName == videoName {
			return rootPath, rootID
		}
		if f.FileCategory == v115open.TypeDir {
			subDirs = append(subDirs, f)
		}
	}
	// 只往下看一层，且限制数量，避免大目录把配额打满
	if len(subDirs) > 8 {
		subDirs = subDirs[:8]
	}
	for _, d := range subDirs {
		sub, serr := t.listDir(d.FileId)
		if serr != nil {
			continue
		}
		for _, f := range sub {
			if f.FileName == videoName {
				return filepath.Join(rootPath, d.FileName), d.FileId
			}
		}
	}
	return rootPath, rootID
}

// listDir 列出 115 目录下的文件与子目录，最多取 200 条。
func (t *MediaInfoSupplement) listDir(dirID string) ([]v115open.File, error) {
	resp, err := t.base.v115Client.GetFsList(t.ctx, dirID, true, false, true, 0, mediaInfoSupplementListPageSize)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return resp.Data, nil
}

// tmpDir 临时目录。放在 avscrape 下是刻意的：AV 上传通道收尾清理空目录时
// 会以 avscrape 为边界向上回退，不会误删到 config/tmp 这一层。
func (t *MediaInfoSupplement) tmpDir() string {
	return filepath.Join(helpers.ConfigDir, "tmp", "avscrape", "mediainfo-supplement", fmt.Sprintf("%d", t.scrapePath.ID))
}

func (t *MediaInfoSupplement) isRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

func (t *MediaInfoSupplement) progress() MediaInfoSupplementProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	errs := make([]string, len(t.errors))
	copy(errs, t.errors)
	return MediaInfoSupplementProgress{
		ScrapePathID: t.scrapePath.ID,
		Running:      t.running,
		Total:        len(t.files),
		Done:         t.done,
		Success:      t.success,
		Skipped:      t.skipped,
		Failed:       t.failed,
		Current:      t.current,
		StartedAt:    t.startedAt,
		FinishedAt:   t.finishedAt,
		Errors:       errs,
	}
}

func (t *MediaInfoSupplement) recordSkip() {
	t.mu.Lock()
	t.skipped++
	t.mu.Unlock()
}

func (t *MediaInfoSupplement) recordFailure(file *models.ScrapeMediaFile, err error) {
	t.mu.Lock()
	t.failed++
	msg := fmt.Sprintf("%s：%v", file.VideoFilename, err)
	if len(t.errors) < mediaInfoSupplementMaxErrors {
		t.errors = append(t.errors, msg)
	}
	t.mu.Unlock()
	helpers.AppLogger.Errorf("[媒体信息补充] %s 失败：%v", file.VideoFilename, err)
}
