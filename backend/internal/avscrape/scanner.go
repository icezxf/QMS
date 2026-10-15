package avscrape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
)

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
	".mov": true, ".flv": true, ".ts": true, ".m2ts": true,
	".iso": true, ".rmvb": true, ".strm": true,
}

var cdPartRegex = regexp.MustCompile(`(?i)([-_]?(cd|part|disc|disk)\d+)`)

// swiperLazyRegex 提取厂商官网 HTML 里 swiper-lazy 类的图（按页面顺序）
var swiperLazyRegex = regexp.MustCompile(
	`(?i)<img[^>]+class=["'][^"']*\bswiper-lazy\b[^"']*["'][^>]+data-src=["']([^"']+\.(?:jpg|jpeg|png|webp))["']`)

var maleActorBlacklist = map[string]bool{
	"清水健": true, "森林原人": true, "しみけん": true, "貞松大輔": true,
	"鮫島健司": true, "吉村卓": true, "冴山トシキ": true, "ウルフ田中": true,
	"マッスル澤野": true, "今井勇太": true, "平井シンジ": true, "橋本真一": true,
	"Shimiken": true, "Ken Shimizu": true, "Daisuke Matsumoto": true, "Taku Yoshimura": true,
}

// ===== 扫描过滤器 =====
var (
	scanFilterMu    sync.Mutex
	scanFilterCodes map[string]bool
)

func SetScanFilter(codes []string) {
	scanFilterMu.Lock()
	defer scanFilterMu.Unlock()
	if len(codes) == 0 {
		scanFilterCodes = nil
		return
	}
	scanFilterCodes = make(map[string]bool, len(codes))
	for _, c := range codes {
		scanFilterCodes[c] = true
	}
}

func takeScanFilter() map[string]bool {
	scanFilterMu.Lock()
	defer scanFilterMu.Unlock()
	if len(scanFilterCodes) == 0 {
		return nil
	}
	cp := make(map[string]bool, len(scanFilterCodes))
	for k, v := range scanFilterCodes {
		cp[k] = v
	}
	scanFilterCodes = nil
	return cp
}

func shouldProcessCodeSnapshot(code string, snapshot map[string]bool) bool {
	if len(snapshot) == 0 {
		return true
	}
	return snapshot[code]
}

func clearScanFilter() {
	scanFilterMu.Lock()
	defer scanFilterMu.Unlock()
	scanFilterCodes = nil
}

// ==========================================================

type Scanner struct {
	DB  *gorm.DB
	Svc *Service

	// probeCache 本次扫描的 ffprobe 结果缓存（key=文件路径）
	probeCache map[string]*probeResult
}

func NewScanner(db *gorm.DB) *Scanner {
	return &Scanner{DB: db, Svc: NewService(db)}
}

type groupItem struct {
	Code  string
	Files []string
}

func (s *Scanner) Scan(pathID uint) error {
	var path models.AVPath
	if err := s.DB.First(&path, pathID).Error; err != nil {
		return err
	}
	if !path.Enable {
		return fmt.Errorf("目录未启用: %d", pathID)
	}

	cfg, cfgErr := LoadConfig(s.DB)
	if cfgErr != nil || cfg == nil {
		def := defaultConfig
		cfg = &def
	}

	fs, err := NewFileSystem(&path)
	if err != nil {
		return fmt.Errorf("创建文件系统失败: %w", err)
	}

	// 初始化缓存
	s.probeCache = make(map[string]*probeResult)

	// 扫描前清理
	helpers.AppLogger.Infof("[AV扫描] 开始清理源目录: %s", path.SourcePath)
	s.cleanupSourceDir(fs, path.SourcePath)

	videoFiles, err := walkVideos(fs, path.SourcePath, 0)
	if err != nil {
		return fmt.Errorf("遍历目录失败: %w", err)
	}
	helpers.AppLogger.Infof("[AV扫描] 目录 %s 共找到 %d 个视频文件", path.SourcePath, len(videoFiles))

	// ============================================================
	// 阶段 1：按父目录粗分组
	// ============================================================
	dirGroups := make(map[string][]string)
	var dirOrder []string
	for _, fullPath := range videoFiles {
		dir := filepath.Dir(fullPath)
		if _, ok := dirGroups[dir]; !ok {
			dirOrder = append(dirOrder, dir)
		}
		dirGroups[dir] = append(dirGroups[dir], fullPath)
	}

	// ============================================================
	// 阶段 2：每组按文件名字典序试 oshash，命中即停
	// ============================================================
	groups := make(map[string]*groupItem)
	var order []string

	for _, dir := range dirOrder {
		files := dirGroups[dir]
		sort.Strings(files)

		matchedCode := ""
		if cfg.EnableOshashMatch && cfg.EnableJavStash {
			for _, f := range files {
				pr := s.getOrProbe(fs, f)
				if pr == nil || pr.Oshash == "" {
					continue
				}
				js := NewJavStashClient(cfg.JavStashEndpoint, cfg.JavStashAPIKey)
				r, err := js.SearchByOshash(pr.Oshash)
				if err == nil && r != nil && r.Code != "" {
					helpers.AppLogger.Infof("[AV扫描] oshash 命中: %s (%s) => %s",
						pr.Oshash, filepath.Base(f), r.Code)
					matchedCode = r.Code
					break
				}
				helpers.AppLogger.Infof("[AV扫描] oshash 未命中: %s (%s)",
					pr.Oshash, filepath.Base(f))
			}
		}

		if matchedCode == "" {
			for _, f := range files {
				code := ExtractCode(filepath.Base(f))
				if code == "" {
					helpers.AppLogger.Warnf("[AV扫描] 无法识别番号: %s", f)
					s.recordTask(0, "", f, "failed", "无法识别番号（oshash 与文件名均失败）", "", nil)
					continue
				}
				if _, ok := groups[code]; !ok {
					groups[code] = &groupItem{Code: code}
					order = append(order, code)
				}
				groups[code].Files = append(groups[code].Files, f)
			}
			continue
		}

		if _, ok := groups[matchedCode]; !ok {
			groups[matchedCode] = &groupItem{Code: matchedCode}
			order = append(order, matchedCode)
		}
		groups[matchedCode].Files = append(groups[matchedCode].Files, files...)
		helpers.AppLogger.Infof("[AV扫描] 目录 %s 整组归入 %s（共 %d 个文件）",
			filepath.Base(dir), matchedCode, len(files))
	}

	// ============================================================
	// 阶段 3：过滤 + 处理
	// ============================================================
	filterSnapshot := takeScanFilter()
	if len(filterSnapshot) > 0 {
		helpers.AppLogger.Infof("[AV扫描] 本次仅处理 %d 个番号", len(filterSnapshot))
	}

	for _, code := range order {
		if !shouldProcessCodeSnapshot(code, filterSnapshot) {
			helpers.AppLogger.Infof("[AV扫描] 番号 %s 不在本次过滤范围内，跳过", code)
			continue
		}

		g := groups[code]

		var existing models.AVMedia
		if err := s.DB.Where("code = ?", code).First(&existing).Error; err == nil {
			if existing.Status == "paused" {
				helpers.AppLogger.Infof("[AV扫描] 番号 %s 处于暂停状态，跳过（原因：%s）", code, existing.PauseReason)
				continue
			}
			if existing.Status == "completed" {
				reason := fmt.Sprintf("重复文件：已存在记录 id=%d，原目标路径=%s",
					existing.ID, existing.TargetPath)
				for _, f := range g.Files {
					s.recordTask(existing.ID, code, f, "duplicate", reason, existing.Source, nil)
				}
				helpers.AppLogger.Warnf("[AV扫描] 番号 %s 重复（已存在 id=%d），共 %d 个文件",
					code, existing.ID, len(g.Files))
				continue
			}
		}

		if err := s.processCode(fs, &path, g, cfg); err != nil {
			helpers.AppLogger.Warnf("[AV扫描] 番号 %s 处理中断: %v", code, err)
			continue
		}
	}

	if path.Mode != "scrape_only" {
		helpers.AppLogger.Infof("[AV扫描] 扫描结束，二次清理源目录: %s", path.SourcePath)
		s.cleanupSourceDir(fs, path.SourcePath)
	}

	s.DB.Model(&path).Update("last_scan_at", time.Now())
	return nil
}

// getOrProbe 查缓存，没有则 ffprobe 并写入缓存
func (s *Scanner) getOrProbe(fs FileSystem, fullPath string) *probeResult {
	if s.probeCache == nil {
		s.probeCache = make(map[string]*probeResult)
	}
	if pr, ok := s.probeCache[fullPath]; ok {
		return pr
	}
	url, err := fs.GetURL(fullPath)
	if err != nil || url == "" {
		helpers.AppLogger.Warnf("[AV扫描] 获取直链失败 %s: %v", filepath.Base(fullPath), err)
		return nil
	}
	pr, err := probeVideo(url)
	if err != nil || pr == nil {
		helpers.AppLogger.Warnf("[AV扫描] ffprobe 失败 %s: %v", filepath.Base(fullPath), err)
		return nil
	}
	s.probeCache[fullPath] = pr
	return pr
}

// ============================================================
// 处理单个番号
// ============================================================

func (s *Scanner) processCode(fs FileSystem, path *models.AVPath, g *groupItem, cfg *Config) error {
	code := g.Code
	primaryFile := g.Files[0]
	helpers.AppLogger.Infof("[AV扫描] 番号 %s 共 %d 个文件，主文件: %s", code, len(g.Files), filepath.Base(primaryFile))

	skipPause := false
	var existingMedia models.AVMedia
	if err := s.DB.Where("code = ?", code).First(&existingMedia).Error; err == nil {
		if existingMedia.Status == "released" {
			skipPause = true
			helpers.AppLogger.Infof("[AV扫描] 番号 %s 已放行，跳过失败检查，强制走完", code)
		}
	}

	// ===== 步骤0：优先用缓存，没有则 ffprobe =====
	oshash := ""
	var probeResult *probeResult

	// ① 优先读源目录 oshash-*.txt
	srcDir := filepath.Dir(primaryFile)
	if entries, err := fs.ListDetailed(srcDir); err == nil {
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			if strings.HasPrefix(e.Name, "oshash-") && strings.HasSuffix(e.Name, ".txt") {
				val := strings.TrimSuffix(strings.TrimPrefix(e.Name, "oshash-"), ".txt")
				if val != "" {
					oshash = val
					helpers.AppLogger.Infof("[AV扫描] 从源目录 oshash 文件读取: %s", oshash)
					break
				}
			}
		}
	}

	// ② 无论 oshash 有没有，只要缓存里有该文件的探测结果，优先复用以拿到分辨率
	if s.probeCache != nil {
		if pr, ok := s.probeCache[primaryFile]; ok && pr != nil {
			probeResult = pr
			if oshash == "" {
				oshash = pr.Oshash
			}
			helpers.AppLogger.Infof("[AV扫描] 复用 ffprobe 缓存: oshash=%s, 分辨率=%s", oshash, pr.Resolution)
		}
	}

	// ③ 如果缓存中也没有，且直链可用，再走 ffprobe
	if probeResult == nil {
		if url, err := fs.GetURL(primaryFile); err == nil && url != "" {
			if pr, err := probeVideo(url); err == nil {
				probeResult = pr
				if oshash == "" {
					oshash = pr.Oshash
				}
				helpers.AppLogger.Infof("[AV扫描] ffprobe 获取成功: 分辨率=%s", pr.Resolution)
			} else {
				helpers.AppLogger.Warnf("[AV扫描] ffprobe 失败: %v", err)
			}
		} else {
			helpers.AppLogger.Warnf("[AV扫描] 获取直链失败: %v", err)
		}
	}

	// ===== 步骤1：刮削元数据 =====
	helpers.AppLogger.Infof("[AV扫描] %s 步骤 1/5: 刮削元数据 (oshash=%s)", code, oshash)
	r, err := s.Svc.Scrape(code, oshash)
	if err != nil {
		reason := fmt.Sprintf("元数据刮削失败: %v", err)
		if skipPause {
			helpers.AppLogger.Warnf("[AV扫描] %s %s（已放行，继续）", code, reason)
			s.recordTask(0, code, primaryFile, "done", reason+"（已放行）", "", nil)
			return nil
		}
		s.pauseMedia(code, primaryFile, reason, "")
		return err
	}
	result := r
	scrapeWarnings := result.Warnings

	if HasTranslateError(scrapeWarnings) {
		reason := "翻译错误（详见警告列表）"
		if skipPause {
			helpers.AppLogger.Warnf("[AV扫描] %s %s（已放行，继续）", code, reason)
		} else {
			s.pauseMediaWithWarnings(code, primaryFile, reason, result.Source, scrapeWarnings)
			return fmt.Errorf("%s", reason)
		}
	}

	media := MediaFromResult(result)

	// 步骤 2 演员过滤
	result.Actors = filterFemaleActors(result.Actors)
	applyEnsembleTag(result)

	// ===== 步骤3：复用步骤0 的 ffprobe 结果 =====
	helpers.AppLogger.Infof("[AV扫描] %s 步骤 2/5: 视频元数据探测", code)
	if probeResult != nil {
		result.Resolution = probeResult.Resolution
		result.IsHDR = probeResult.IsHDR
		result.Oshash = probeResult.Oshash
		helpers.AppLogger.Infof("[AV探测] %s 复用 ffprobe: 分辨率=%s HDR=%v oshash=%s",
			result.Code, result.Resolution, result.IsHDR, result.Oshash)
	} else if result.Oshash == "" {
		result.Oshash = oshash
	}
	s.detectVideoMetaOnly(fs, primaryFile, result, cfg)

	if result.Resolution == "" {
		reason := "ffprobe 探测失败，未获取到分辨率"
		if skipPause {
			helpers.AppLogger.Warnf("[AV扫描] %s %s（已放行，继续）", code, reason)
		} else {
			s.pauseMediaWithWarnings(code, primaryFile, reason, result.Source, scrapeWarnings)
			return fmt.Errorf("%s", reason)
		}
	}

	// 步骤 4 元数据文件
	helpers.AppLogger.Infof("[AV扫描] %s 步骤 3/5: 生成元数据文件", code)
	nfoBaseName := media.Code + resolutionSuffix(result.Resolution)
	files, prepWarnings, err := s.prepareMetaFiles(nfoBaseName, result, cfg, path.IsVR)
	allWarnings := append([]string{}, scrapeWarnings...)
	allWarnings = append(allWarnings, prepWarnings...)
	if err != nil {
		reason := fmt.Sprintf("生成元数据文件失败: %v", err)
		if skipPause {
			helpers.AppLogger.Warnf("[AV扫描] %s %s（已放行，继续）", code, reason)
		} else {
			s.pauseMediaWithWarnings(code, primaryFile, reason, result.Source, allWarnings)
			return err
		}
	}

	// 步骤 5 完整性
	helpers.AppLogger.Infof("[AV扫描] %s 步骤 4/5: 检查元数据完整性", code)
	if missing := checkMetadataMissing(result); missing != "" {
		reason := fmt.Sprintf("元数据缺失: %s", missing)
		if skipPause {
			helpers.AppLogger.Warnf("[AV扫描] %s %s（已放行，继续）", code, reason)
		} else {
			s.pauseMediaWithWarnings(code, primaryFile, reason, result.Source, allWarnings)
			return fmt.Errorf("%s", reason)
		}
	}

	// 步骤 6 整理
	helpers.AppLogger.Infof("[AV扫描] %s 步骤 5/5: 整理视频文件", code)
	media = MediaFromResult(result)
	targetDir, anySkipped, err := s.organize(fs, path, media, g.Files, result, cfg)
	if err != nil {
		reason := fmt.Sprintf("整理失败: %v", err)
		if skipPause {
			helpers.AppLogger.Warnf("[AV扫描] %s %s（已放行，继续）", code, reason)
		} else {
			s.pauseMediaWithWarnings(code, primaryFile, reason, result.Source, allWarnings)
			return err
		}
	}

	if anySkipped {
		reason := fmt.Sprintf("目标视频已存在：%s", targetDir)
		for _, f := range g.Files {
			s.recordTask(0, code, f, "duplicate", reason, result.Source, nil)
		}
		helpers.AppLogger.Warnf("[AV扫描] 番号 %s 目标视频已存在，整个跳过", code)
		return nil
	}

	// 步骤 7 上传
	if len(files) > 0 {
		if _, err := fs.QueueUploads(files, targetDir, path.AccountID, path.SourceType); err != nil {
			reason := fmt.Sprintf("排队上传元数据失败: %v", err)
			s.pauseMediaWithWarnings(code, primaryFile, reason, result.Source, allWarnings)
			return err
		}
	}

	s.recordTask(0, code, primaryFile, "done", "", result.Source, allWarnings)
	helpers.AppLogger.Infof("[AV扫描] 番号 %s 处理完成", code)
	return nil
}

func checkMetadataMissing(r *ScrapeResult) string {
	if r == nil {
		return "ScrapeResult 为空"
	}
	var missing []string
	if r.Title == "" {
		missing = append(missing, "Title")
	}
	if r.Plot == "" {
		missing = append(missing, "Plot")
	}
	if r.Poster == "" {
		missing = append(missing, "Poster")
	}
	if r.Fanart == "" {
		missing = append(missing, "Fanart")
	}
	if len(r.Actors) == 0 {
		missing = append(missing, "Actors")
	}
	if r.Rating == 0 {
		missing = append(missing, "Rating")
	}
	return strings.Join(missing, ", ")
}

func (s *Scanner) pauseMedia(code, filePath, reason, provider string) {
	s.pauseMediaWithWarnings(code, filePath, reason, provider, nil)
}

func (s *Scanner) pauseMediaWithWarnings(code, filePath, reason, provider string, warnings []string) {
	helpers.AppLogger.Warnf("[AV暂停] %s: %s", code, reason)

	var existing models.AVMedia
	err := s.DB.Where("code = ?", code).First(&existing).Error
	if err == nil {
		s.DB.Model(&existing).Updates(map[string]any{
			"status":       "paused",
			"pause_reason": reason,
			"paused_at":    time.Now(),
		})
		s.recordTask(existing.ID, code, filePath, "paused", reason, provider, warnings)
	} else {
		m := &models.AVMedia{
			Code:        code,
			Status:      "paused",
			PauseReason: reason,
			PausedAt:    time.Now(),
		}
		s.DB.Create(m)
		s.recordTask(m.ID, code, filePath, "paused", reason, provider, warnings)
	}
}

func (s *Scanner) markCompleted(code, targetPath string) {
	s.DB.Model(&models.AVMedia{}).Where("code = ?", code).Updates(map[string]any{
		"status":         "completed",
		"pause_reason":   "",
		"progress_stage": "",
		"target_path":    targetPath,
	})
}

func filterFemaleActors(actors []Actor) []Actor {
	out := make([]Actor, 0, len(actors))
	for _, a := range actors {
		if maleActorBlacklist[a.Name] {
			continue
		}
		skip := false
		for _, alias := range a.Aliases {
			if maleActorBlacklist[alias] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		out = append(out, a)
	}
	return out
}

func applyEnsembleTag(r *ScrapeResult) {
	if r == nil || len(r.Actors) < 2 {
		return
	}
	has := false
	for _, g := range r.Genres {
		if g == "共演" {
			has = true
			break
		}
	}
	if !has {
		r.Genres = append(r.Genres, "共演")
	}
	if r.Series == "" {
		r.Series = "共演"
	} else if !strings.Contains(r.Series, "共演") {
		r.Series = r.Series + ",共演"
	}
}

func (s *Scanner) detectVideoMetaOnly(fs FileSystem, fullPath string, r *ScrapeResult, cfg *Config) {
	r.IsUncensored = detectUncensored(r.Code)
	r.HasChineseSub = detectChineseSub(fs, fullPath)
	r.ExtraTags = buildExtraTags(r, cfg)
	if len(r.ExtraTags) > 0 {
		helpers.AppLogger.Infof("[AV探测] %s 附加标签: %v", r.Code, r.ExtraTags)
	}
}

func resolutionSuffix(res string) string {
	switch res {
	case "8K":
		return "-8K"
	case "7K":
		return "-7K"
	case "6K":
		return "-6K"
	case "5K":
		return "-5K"
	case "4K":
		return "-4K"
	}
	return ""
}

func (s *Scanner) organize(fs FileSystem, path *models.AVPath, media *models.AVMedia, videoPaths []string, r *ScrapeResult, cfg *Config) (string, bool, error) {
	relDir := renderTemplate(path.NameTemplate, media)
	if relDir == "" {
		relDir = media.Code
	}
	targetDir := strings.TrimRight(path.TargetPath, "/") + "/" + relDir
	if err := fs.MkdirAll(targetDir); err != nil {
		return "", false, err
	}
	helpers.AppLogger.Infof("[AV整理] 目标目录: %s", targetDir)

	resSuffix := ""
	if r != nil {
		resSuffix = resolutionSuffix(r.Resolution)
	}

	anySkipped := false

	for _, srcPath := range videoPaths {
		originalName := filepath.Base(srcPath)
		ext := filepath.Ext(originalName)
		baseName := strings.TrimSuffix(originalName, ext)

		cdSuffix := ""
		if m := cdPartRegex.FindString(baseName); m != "" {
			cdSuffix = strings.ToLower(m)
		}

		newName := media.Code + resSuffix + cdSuffix + ext
		newPath := targetDir + "/" + newName

		if fs.Exists(newPath) {
			helpers.AppLogger.Infof("[AV整理] 目标已存在，跳过: %s", newName)
			anySkipped = true
			continue
		}

		switch path.MoveMethod {
		case "copy":
			if err := fs.Copy(srcPath, targetDir); err != nil {
				helpers.AppLogger.Warnf("[AV整理] 复制失败 %s: %v", srcPath, err)
				continue
			}
			if originalName != newName {
				oldPath := targetDir + "/" + originalName
				if err := fs.Rename(oldPath, newName); err != nil {
					helpers.AppLogger.Warnf("[AV整理] 重命名失败 %s: %v", oldPath, err)
				}
			}
		default:
			if err := fs.Move(srcPath, targetDir, newName); err != nil {
				helpers.AppLogger.Warnf("[AV整理] 移动失败 %s: %v", srcPath, err)
				continue
			}
		}
		helpers.AppLogger.Infof("[AV整理] %s → %s", originalName, newName)
	}

	return targetDir, anySkipped, nil
}

func (s *Scanner) prepareMetaFiles(baseName string, r *ScrapeResult, cfg *Config, isVR bool) ([]LocalFile, []string, error) {
	var warnings []string
	if r == nil {
		return nil, warnings, nil
	}

	tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "avscrape", baseName)
	os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, warnings, fmt.Errorf("创建临时目录失败: %w", err)
	}

	files := []LocalFile{}

	watermarks := buildWatermarks(r, cfg)
	if len(watermarks) > 0 {
		names := make([]string, 0, len(watermarks))
		for _, w := range watermarks {
			names = append(names, w.Label)
		}
		helpers.AppLogger.Infof("[AV水印] %s 准备打水印: %v", r.Code, names)
	}

	var posterData, fanartData, thumbData []byte
	const minPosterWidth = 500

	// ===== ① ImageCandidates 筛选（H>W 且 W>=500 → poster；H<W → fanart）=====
	candidates := r.ImageCandidates
	if len(candidates) == 0 {
		if r.Poster != "" {
			candidates = append(candidates, r.Poster)
		}
		if r.Fanart != "" && r.Fanart != r.Poster {
			candidates = append(candidates, r.Fanart)
		}
		helpers.AppLogger.Infof("[AV元数据] ImageCandidates 为空，用 r.Poster/r.Fanart 兜底，共 %d 张", len(candidates))
	}

	helpers.AppLogger.Infof("[AV元数据] 待筛选图片共 %d 张:", len(candidates))
	for i, url := range candidates {
		data, err := downloadImage(url)
		if err != nil {
			helpers.AppLogger.Warnf("[AV元数据]   [%d] 下载失败: %s => %v", i, redactURL(url), err)
			warnings = append(warnings, fmt.Sprintf("图片下载失败: %s", redactURL(url)))
			continue
		}
		imgCfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			helpers.AppLogger.Warnf("[AV元数据]   [%d] 解码失败: %v", i, err)
			continue
		}
		helpers.AppLogger.Infof("[AV元数据]   [%d] %dx%d (%s)", i, imgCfg.Width, imgCfg.Height, redactURL(url))
		if imgCfg.Height > imgCfg.Width {
			if imgCfg.Width >= minPosterWidth {
				if posterData == nil {
					posterData = data
					helpers.AppLogger.Infof("[AV元数据]   → 作为 poster (%dx%d)", imgCfg.Width, imgCfg.Height)
				}
			} else {
				helpers.AppLogger.Infof("[AV元数据]   → 竖版但低清 (%dx%d < %d)，丢弃", imgCfg.Width, imgCfg.Height, minPosterWidth)
			}
		}
		if fanartData == nil && imgCfg.Height < imgCfg.Width {
			fanartData = data
			r.Fanart = url
			helpers.AppLogger.Infof("[AV元数据]   → 作为 fanart")
		}
		if posterData != nil && fanartData != nil {
			helpers.AppLogger.Infof("[AV元数据] poster 和 fanart 都找到了，停止遍历")
			break
		}
	}

	if posterData == nil {
		helpers.AppLogger.Warnf("[AV元数据] ImageCandidates 里没有高清竖版图，将走兜底")
	}

	// 暂存原始 fanart 给 thumb 用（DMM 替换时不跟着变）
	thumbData = fanartData

	// ===== ② DMM 封面兜底（用 r18 给的 content_id 原样拼 mono + digital）=====
	const dmmMinWidth = 500
	if posterData == nil && len(r.DMMContentIDs) > 0 {
		var bestData []byte
		var bestW, bestH int
		var bestURL string

		for _, url := range dmmCoverCandidates(r.Code, r.DMMContentIDs) {
			data, err := downloadImage(url)
			if err != nil {
				helpers.AppLogger.Infof("[AV元数据] DMM 候选失败: %s => %v", redactURL(url), err)
				continue
			}
			cfg2, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				helpers.AppLogger.Infof("[AV元数据] DMM 图解码失败: %s", redactURL(url))
				continue
			}
			if cfg2.Width < dmmMinWidth {
				helpers.AppLogger.Infof("[AV元数据] DMM 图宽度不足 %dpx (%dx%d)，跳过: %s",
					dmmMinWidth, cfg2.Width, cfg2.Height, redactURL(url))
				continue
			}
			if cfg2.Width*cfg2.Height > bestW*bestH {
				bestData = data
				bestW = cfg2.Width
				bestH = cfg2.Height
				bestURL = url
			}
		}

		if bestData != nil {
			posterData = bestData
			helpers.AppLogger.Infof("[AV元数据] poster 来自 DMM: %dx%d (%s)", bestW, bestH, redactURL(bestURL))
			warnings = append(warnings, fmt.Sprintf("poster 来自 DMM (%dx%d)", bestW, bestH))

			// ===== 用 DMM 的 pl.jpg 替换 fanart（thumb 不变） =====
			plURL := strings.Replace(bestURL, "ps.jpg", "pl.jpg", 1)
			if plURL != bestURL {
				if plData, err := downloadImage(plURL); err == nil {
					if plCfg, _, err := image.DecodeConfig(bytes.NewReader(plData)); err == nil {
						if plCfg.Height < plCfg.Width {
							fanartData = plData
							r.Fanart = plURL
							helpers.AppLogger.Infof("[AV元数据] fanart 替换为 DMM: %dx%d (%s)",
								plCfg.Width, plCfg.Height, redactURL(plURL))
							warnings = append(warnings,
								fmt.Sprintf("fanart 来自 DMM (%dx%d)", plCfg.Width, plCfg.Height))
						} else {
							helpers.AppLogger.Infof("[AV元数据] DMM pl.jpg 不是横版 (%dx%d)，保留原 fanart",
								plCfg.Width, plCfg.Height)
						}
					}
				} else {
					helpers.AppLogger.Infof("[AV元数据] DMM pl.jpg 下载失败: %v", err)
				}
			}
		}
	}

	// ===== ②.5 厂商官网封面兜底（仅 VR） =====
	// JavStash 返回的 urls 顺序随机，r18.dev / dmm 等聚合站可能排第一，
	// 必须过滤掉聚合站，只取真正的厂商官网
	if posterData == nil && isVR && len(r.Urls) > 0 {
		officialURL := pickOfficialURL(r.Urls)
		if officialURL != "" {
			helpers.AppLogger.Infof("[AV元数据] VR 兜底：尝试从厂商官网抓封面: %s", redactURL(officialURL))
			if data, w, h, src := fetchOfficialCoverData(officialURL); data != nil {
				posterData = data
				helpers.AppLogger.Infof("[AV元数据] poster 来自厂商官网 (%dx%d)", w, h)
				warnings = append(warnings,
					fmt.Sprintf("⚠️ poster 使用厂商官网兜底 (%dx%d): %s", w, h, redactURL(src)))
			} else {
				helpers.AppLogger.Infof("[AV元数据] 厂商官网未找到合适封面")
			}
		} else {
			helpers.AppLogger.Infof("[AV元数据] VR 兜底：r.Urls 全是聚合站（无厂商官网），跳过")
			warnings = append(warnings, "VR poster 兜底失败：JavStash 未提供厂商官网 URL")
		}
	}

	// ===== ③ VR 分流：VR 不走 fanart 裁剪 =====
	if posterData == nil && !isVR && fanartData != nil {
		if cropped, ok := cropPosterFromFanart(fanartData); ok {
			posterData = cropped
			helpers.AppLogger.Infof("[AV元数据] poster 从 fanart 右侧裁剪")
			warnings = append(warnings, "poster 通过 fanart 右侧裁剪获得（原始源无竖版图）")
		} else {
			helpers.AppLogger.Warnf("[AV元数据] fanart 裁剪 poster 失败")
			warnings = append(warnings, "fanart 裁剪 poster 失败")
		}
	}

	// ===== ④ resize =====
	if posterData != nil {
		if resized, ok := resizePosterToMin(posterData, 500, 750); ok {
			posterData = resized
		}
	}

	// ===== ⑤ 水印 =====
	if posterData != nil && len(watermarks) > 0 {
		if wm, err := applyWatermark(posterData, watermarks, cfg.WatermarkWidthPercent); err == nil {
			posterData = wm
			helpers.AppLogger.Infof("[AV水印] poster 已打水印")
		}
	}
	if fanartData != nil && len(watermarks) > 0 {
		if wm, err := applyWatermark(fanartData, watermarks, cfg.WatermarkWidthPercent); err == nil {
			fanartData = wm
			helpers.AppLogger.Infof("[AV水印] fanart 已打水印")
		}
	}
	if thumbData != nil && len(watermarks) > 0 {
		if wm, err := applyWatermark(thumbData, watermarks, cfg.WatermarkWidthPercent); err == nil {
			thumbData = wm
			helpers.AppLogger.Infof("[AV水印] thumb 已打水印")
		}
	}

	// ===== ⑥ 写文件 =====
	if posterData != nil {
		p := filepath.Join(tmpDir, "poster.jpg")
		if err := os.WriteFile(p, posterData, 0644); err == nil {
			files = append(files, LocalFile{LocalPath: p, RemoteName: "poster.jpg"})
			r.Poster = "poster.jpg"
		}
	} else {
		r.Poster = ""
		helpers.AppLogger.Warnf("[AV元数据] poster 最终为 nil，未生成")
		warnings = append(warnings, "poster 最终未生成")
	}

	if fanartData != nil {
		p := filepath.Join(tmpDir, "fanart.jpg")
		if err := os.WriteFile(p, fanartData, 0644); err == nil {
			files = append(files, LocalFile{LocalPath: p, RemoteName: "fanart.jpg"})
			r.Fanart = "fanart.jpg"
		}
	} else {
		r.Fanart = ""
		helpers.AppLogger.Warnf("[AV元数据] fanart 最终为 nil，未生成")
		warnings = append(warnings, "fanart 最终未生成")
	}

	// thumb 独立于 fanart（DMM 替换 fanart 时 thumb 保留 javstash 原图）
	if thumbData != nil {
		p2 := filepath.Join(tmpDir, "thumb.jpg")
		if err := os.WriteFile(p2, thumbData, 0644); err == nil {
			files = append(files, LocalFile{LocalPath: p2, RemoteName: "thumb.jpg"})
		}
	} else if fanartData != nil {
		p2 := filepath.Join(tmpDir, "thumb.jpg")
		if err := os.WriteFile(p2, fanartData, 0644); err == nil {
			files = append(files, LocalFile{LocalPath: p2, RemoteName: "thumb.jpg"})
		}
	}

	// ===== 剧照下载 =====
	helpers.AppLogger.Infof("[AV元数据] PreviewImages 共 %d 张", len(r.PreviewImages))
	successCount := 0
	failCount := 0
	for i, url := range r.PreviewImages {
		remoteName := fmt.Sprintf("extrafanart/fanart%d.jpg", i+1)
		localPath := filepath.Join(tmpDir, fmt.Sprintf("fanart%d.jpg", i+1))
		if err := helpers.DownloadFile(url, localPath, ""); err == nil {
			files = append(files, LocalFile{LocalPath: localPath, RemoteName: remoteName})
			successCount++
		} else {
			helpers.AppLogger.Warnf("[AV元数据] 下载剧照 %d 失败: %v", i+1, err)
			failCount++
		}
	}

	// ===== DMM 剧照兜底（仅当数据源一张剧照都没给时）=====
	const dmmPreviewMax = 15
	needDMM := len(r.PreviewImages) == 0
	if needDMM && len(r.DMMContentIDs) > 0 {
		remaining := dmmPreviewMax - successCount
		if remaining > 0 {
			for _, cid := range r.DMMContentIDs {
				if remaining <= 0 {
					break
				}
				count := downloadDMMPreviews(cid, "digital", remaining, successCount, tmpDir, &files)
				if count == 0 {
					count = downloadDMMPreviews(cid, "mono", remaining, successCount, tmpDir, &files)
				}
				if count > 0 {
					helpers.AppLogger.Infof("[AV元数据] DMM 剧照兜底[%s] 补充 %d 张", cid, count)
					warnings = append(warnings, fmt.Sprintf("剧照通过 DMM(%s) 兜底补充 %d 张", cid, count))
					successCount += count
					remaining -= count
				}
			}
		}
	}

	if successCount > 0 {
		helpers.AppLogger.Infof("[AV元数据] 剧照总数: %d 张", successCount)
		if failCount > 0 {
			warnings = append(warnings, fmt.Sprintf("剧照部分下载失败: 成功 %d, 失败 %d", successCount, failCount))
		}
	}

	// ===== oshash 文件 =====
	if r.Oshash != "" {
		fileName := fmt.Sprintf("oshash-%s.txt", r.Oshash)
		p := filepath.Join(tmpDir, fileName)
		if err := os.WriteFile(p, []byte(r.Oshash+"\n"), 0644); err == nil {
			files = append(files, LocalFile{LocalPath: p, RemoteName: fileName})
			helpers.AppLogger.Infof("[AV元数据] 生成 oshash 文件: %s", fileName)
		}
	}

	if r.Trailer != "" {
		p := filepath.Join(tmpDir, "trailer.strm")
		if err := os.WriteFile(p, []byte(r.Trailer), 0644); err == nil {
			files = append(files, LocalFile{LocalPath: p, RemoteName: "trailers/trailer.strm"})
		}
	}

	nfoPath := filepath.Join(tmpDir, baseName+".nfo")
	if err := os.WriteFile(nfoPath, []byte(GenerateNFO(r)), 0644); err != nil {
		return nil, warnings, fmt.Errorf("写 NFO 失败: %w", err)
	}
	files = append(files, LocalFile{LocalPath: nfoPath, RemoteName: baseName + ".nfo"})

	return files, warnings, nil
}

// ============================================================
// DMM 封面 / 剧照辅助函数
// ============================================================

func dmmCoverCandidates(code string, contentIDs []string) []string {
	seen := map[string]bool{}
	var urls []string
	add := func(u string) {
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		urls = append(urls, u)
	}

	addOneCID := func(cid string) {
		raw := strings.ToLower(cid)
		add(fmt.Sprintf("https://awsimgsrc.dmm.com/dig/mono/movie/%s/%sps.jpg", raw, raw))
		add(fmt.Sprintf("https://awsimgsrc.dmm.com/dig/digital/video/%s/%sps.jpg", raw, raw))
	}

	if len(contentIDs) > 0 {
		for _, cid := range contentIDs {
			addOneCID(cid)
		}
	} else {
		raw := strings.ToLower(strings.ReplaceAll(code, "-", ""))
		if raw != "" {
			addOneCID(raw)
		}
	}
	return urls
}

func downloadDMMPreviews(cid, variant string, max int, startIdx int, tmpDir string, files *[]LocalFile) int {
	count := 0
	for i := 1; i <= max; i++ {
		var url string
		if variant == "digital" {
			url = fmt.Sprintf("https://awsimgsrc.dmm.co.jp/pics_dig/digital/video/%s/%sjp-%d.jpg", cid, cid, i)
		} else {
			url = fmt.Sprintf("https://awsimgsrc.dmm.com/dig/mono/movie/%s/%sjp-%d.jpg", cid, cid, i)
		}
		data, err := downloadImage(url)
		if err != nil {
			break
		}
		idx := startIdx + count + 1
		localPath := filepath.Join(tmpDir, fmt.Sprintf("fanart%d.jpg", idx))
		if err := os.WriteFile(localPath, data, 0644); err != nil {
			continue
		}
		remoteName := fmt.Sprintf("extrafanart/fanart%d.jpg", idx)
		*files = append(*files, LocalFile{LocalPath: localPath, RemoteName: remoteName})
		count++
	}
	return count
}

// ============================================================
// 厂商官网封面兜底（仅 VR）
// ============================================================

func normalizeURL(base, u string) string {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		if b, err := url.Parse(base); err == nil {
			return b.Scheme + "://" + b.Host + u
		}
	}
	return ""
}

func tryDownloadCover(imgURL, referer string) ([]byte, int, int, bool) {
	req, err := http.NewRequest("GET", imgURL, nil)
	if err != nil {
		return nil, 0, 0, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, 0, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil || len(data) < 1000 {
		return nil, 0, 0, false
	}
	imgCfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, false
	}
	if imgCfg.Height <= imgCfg.Width {
		return nil, imgCfg.Width, imgCfg.Height, false
	}
	if imgCfg.Width < 500 {
		return nil, imgCfg.Width, imgCfg.Height, false
	}
	ratio := float64(imgCfg.Width) / float64(imgCfg.Height)
	if ratio < 0.70 {
		return nil, imgCfg.Width, imgCfg.Height, false
	}
	return data, imgCfg.Width, imgCfg.Height, true
}

func fetchOfficialCoverData(pageURL string) ([]byte, int, int, string) {
	if pageURL == "" {
		return nil, 0, 0, ""
	}
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, 0, 0, ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ja,en;q=0.9,zh;q=0.8")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		helpers.AppLogger.Warnf("[AV元数据] 厂商官网请求失败 %s: %v", redactURL(pageURL), err)
		return nil, 0, 0, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		helpers.AppLogger.Warnf("[AV元数据] 厂商官网 HTTP %d: %s", resp.StatusCode, redactURL(pageURL))
		return nil, 0, 0, ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, 0, 0, ""
	}
	html := string(body)

	var candidates []string
	seen := map[string]bool{}
	for _, m := range swiperLazyRegex.FindAllStringSubmatch(html, -1) {
		if u := normalizeURL(pageURL, m[1]); u != "" && !seen[u] {
			candidates = append(candidates, u)
			seen[u] = true
		}
	}
	helpers.AppLogger.Infof("[AV元数据] 厂商官网提取到 %d 张 swiper-lazy 图", len(candidates))
	if len(candidates) == 0 {
		return nil, 0, 0, ""
	}

	type result struct {
		data  []byte
		w, h  int
		src   string
		ratio float64
	}
	var best *result

	for i, u := range candidates {
		data, w, h, ok := tryDownloadCover(u, pageURL)
		if !ok {
			if w > 0 {
				helpers.AppLogger.Infof("[AV元数据]   跳过第 %d 张 (%dx%d, 比例 %.3f): %s",
					i+1, w, h, float64(w)/float64(h), redactURL(u))
			}
			continue
		}
		ratio := float64(w) / float64(h)
		helpers.AppLogger.Infof("[AV元数据]   候选第 %d 张 (%dx%d, 比例 %.3f): %s",
			i+1, w, h, ratio, redactURL(u))
		if best == nil || ratio > best.ratio {
			best = &result{data: data, w: w, h: h, src: u, ratio: ratio}
		}
	}

	if best == nil {
		helpers.AppLogger.Infof("[AV元数据] 厂商官网没有合适的竖版封面")
		return nil, 0, 0, ""
	}
	helpers.AppLogger.Infof("[AV元数据] 厂商封面命中 (比例最大 %dx%d, %.3f): %s",
		best.w, best.h, best.ratio, redactURL(best.src))
	return best.data, best.w, best.h, best.src
}

func (s *Scanner) recordTask(mediaId uint, code, filePath, status, msg, provider string, warnings []string) {
	warningsJSON, _ := json.Marshal(warnings)
	s.DB.Create(&models.AVTask{
		MediaId:  mediaId,
		Code:     code,
		FilePath: filePath,
		Status:   status,
		Message:  msg,
		Provider: provider,
		Warnings: string(warningsJSON),
	})
}

func walkVideos(fs FileSystem, root string, depth int) ([]string, error) {
	if depth > 10 {
		return nil, nil
	}
	entries, err := fs.ListDetailed(root)
	if err != nil {
		return nil, err
	}
	var videos []string
	for _, e := range entries {
		if e.IsDir {
			lower := strings.ToLower(e.Name)
			if lower == "extrafanart" || lower == "trailers" ||
				lower == "backdrops" || lower == "thumbnails" ||
				lower == "season" || strings.HasPrefix(lower, "season ") {
				continue
			}
			sub, err := walkVideos(fs, e.Path, depth+1)
			if err == nil {
				videos = append(videos, sub...)
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name))
		if !videoExts[ext] {
			continue
		}
		if e.Size == 0 || e.Size >= cleanupBigVideoSize {
			videos = append(videos, e.Path)
		}
	}
	return videos, nil
}

func renderTemplate(tpl string, media *models.AVMedia) string {
	if tpl == "" {
		return media.Code
	}
	var actors []Actor
	if media.Actors != "" {
		_ = json.Unmarshal([]byte(media.Actors), &actors)
	}

	names := make([]string, 0, len(actors))
	for _, a := range actors {
		name := pickBestActorName(a)
		if name != "" {
			names = append(names, name)
		}
	}

	actorDir := ""
	switch {
	case len(names) == 0:
		actorDir = "未知演员"
	case len(names) <= 3:
		actorDir = strings.Join(names, ",")
	default:
		actorDir = "多人作品"
	}

	const maxActorsInPath = 5
	allActorsPath := ""
	switch {
	case len(names) == 0:
		allActorsPath = "未知演员"
	case len(names) == 1:
		allActorsPath = sanitizePath(names[0])
	default:
		head := names
		suffix := ""
		if len(names) > maxActorsInPath {
			head = names[:maxActorsInPath]
			suffix = fmt.Sprintf("等%d人", len(names))
		}
		allActorsPath = "多人作品/" + sanitizePath(strings.Join(head, ", ")) + suffix
	}

	replacer := strings.NewReplacer(
		"{actor}", sanitizePath(actorDir),
		"{actors}", allActorsPath,
		"{number}", sanitizePath(media.Code),
		"{code}", sanitizePath(media.Code),
		"{title}", sanitizePath(media.Title),
		"{year}", extractYear(media.ReleaseDate),
		"{studio}", sanitizePath(media.Studio),
		"{label}", sanitizePath(media.Label),
		"{series}", sanitizePath(media.Series),
		"{director}", sanitizePath(media.Director),
		"{resolution}", sanitizePath(media.Resolution),
	)

	result := replacer.Replace(tpl)
	result = strings.Trim(result, "/")
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}
	parts := strings.Split(result, "/")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "/")
}

func pickBestActorName(a Actor) string {
	if a.Name != "" {
		return a.Name
	}
	if len(a.Aliases) > 0 && a.Aliases[0] != "" {
		return a.Aliases[0]
	}
	return ""
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func sanitizePath(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "：", "*", "_",
		"?", "？", "\"", "'", "<", "《", ">", "》", "|", "_",
	)
	return strings.TrimSpace(replacer.Replace(s))
}

func extractYear(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

// ============================================================
// 厂商官网 URL 过滤（聚合站黑名单）
// ============================================================

// aggregatorHosts 聚合站/数据库站黑名单，这些不是厂商官网
var aggregatorHosts = []string{
	"r18.dev",
	"dmm.co.jp",
	"dmm.com",
	"fanza.com",
	"fanza.co.jp",
	"javdb.com",
	"javbus.com",
	"javstash.org",
	"stashdb.org",
	"jav321.com",
	"avmoo",
	"seesaawiki",
	"wikipedia.org",
}

// isAggregatorHost 判断 URL 是否属于聚合站
func isAggregatorHost(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Host)
	for _, a := range aggregatorHosts {
		if strings.Contains(host, a) {
			return true
		}
	}
	return false
}

// pickOfficialURL 从 urls 列表里挑出第一个真正的厂商官网
// JavStash 返回的 urls 顺序随机，所以需要全部遍历，跳过黑名单
func pickOfficialURL(urls []string) string {
	for _, u := range urls {
		if !isAggregatorHost(u) {
			return u
		}
	}
	return ""
}
