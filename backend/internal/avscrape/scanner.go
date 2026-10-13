package avscrape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
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

	// 扫描前清理
	helpers.AppLogger.Infof("[AV扫描] 开始清理源目录: %s", path.SourcePath)
	s.cleanupSourceDir(fs, path.SourcePath)

	videoFiles, err := walkVideos(fs, path.SourcePath, 0)
	if err != nil {
		return fmt.Errorf("遍历目录失败: %w", err)
	}
	helpers.AppLogger.Infof("[AV扫描] 目录 %s 共找到 %d 个视频文件", path.SourcePath, len(videoFiles))

	groups := make(map[string]*groupItem)
	var order []string
	for _, fullPath := range videoFiles {
		name := filepath.Base(fullPath)
		code := ExtractCode(name)
		if code == "" {
			helpers.AppLogger.Warnf("[AV扫描] 无法识别番号: %s", fullPath)
			s.recordTask(0, "", fullPath, "failed", "无法识别番号", "", nil)
			continue
		}
		if _, ok := groups[code]; !ok {
			groups[code] = &groupItem{Code: code}
			order = append(order, code)
		}
		groups[code].Files = append(groups[code].Files, fullPath)
	}

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

	// ===== 步骤0：获取 oshash（优先读源目录 oshash-*.txt，没有则 ffprobe）=====
	oshash := ""
	var probeResult *ProbeResult

	// ① 优先读源目录 oshash-*.txt（从文件名提取 oshash 值）
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

	// ② 没有则 ffprobe
	if oshash == "" {
		if url, err := fs.GetURL(primaryFile); err == nil && url != "" {
			if pr, err := probeVideo(url); err == nil {
				probeResult = pr
				oshash = pr.Oshash
				helpers.AppLogger.Infof("[AV扫描] ffprobe 获取 oshash: %s", oshash)
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

	// 步骤 1.5 翻译错误
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

	// 视频已存在 → 整个跳过
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
			reason := fmt.Sprintf("加入上传队列失败: %v", err)
			if skipPause {
				helpers.AppLogger.Warnf("[AV扫描] %s %s（已放行，继续）", code, reason)
			} else {
				s.pauseMediaWithWarnings(code, primaryFile, reason, result.Source, allWarnings)
				return err
			}
		}
	}

	// 成功
	s.markCompleted(code, targetDir)
	s.recordTask(existingMedia.ID, code, primaryFile, "done", fmt.Sprintf("共 %d 个文件", len(g.Files)), result.Source, allWarnings)
	helpers.AppLogger.Infof("[AV扫描] %s 刮削完成", code)
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

// detectVideoMetaOnly 只做非 ffprobe 部分（ffprobe 已在步骤0 提前完成）
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

// organize 移动视频
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

	var posterData, fanartData []byte

	// ===== ① ImageCandidates 筛选（H>W → poster，H<W → fanart）=====
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
		if posterData == nil && imgCfg.Height > imgCfg.Width {
			posterData = data
			helpers.AppLogger.Infof("[AV元数据]   → 作为 poster")
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
		helpers.AppLogger.Warnf("[AV元数据] ImageCandidates 里没有竖版图，将走兜底")
	}

	// ===== ② DMM 封面兜底（r18 给的 content_id 原样拼 mono + digital）=====
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
		p2 := filepath.Join(tmpDir, "thumb.jpg")
		if err := os.WriteFile(p2, fanartData, 0644); err == nil {
			files = append(files, LocalFile{LocalPath: p2, RemoteName: "thumb.jpg"})
		}
	} else {
		r.Fanart = ""
		helpers.AppLogger.Warnf("[AV元数据] fanart 最终为 nil，未生成")
		warnings = append(warnings, "fanart 最终未生成")
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

	// ===== DMM 剧照兜底（VR 总是走；非 VR 剧照 < 8 张时走）=====
	const dmmPreviewMax = 15
	needDMM := isVR || successCount < 8
	if needDMM && len(r.DMMContentIDs) > 0 {
		remaining := dmmPreviewMax - successCount
		if remaining > 0 {
			for _, cid := range r.DMMContentIDs {
				if remaining <= 0 {
					break
				}
				// 先试 digital
				count := downloadDMMPreviews(cid, "digital", remaining, successCount, tmpDir, &files)
				if count == 0 {
					// digital 全失败，试 mono
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

// dmmCoverCandidates 生成 DMM 封面候选 URL
// r18 给的 content_id 原样使用，每个 id 生成 mono + digital 两个 URL
// 全部下载，调用方比分辨率选最高清
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
		// mono 实体版
		add(fmt.Sprintf("https://awsimgsrc.dmm.com/dig/mono/movie/%s/%sps.jpg", raw, raw))
		// digital 数字版
		add(fmt.Sprintf("https://awsimgsrc.dmm.com/dig/digital/video/%s/%sps.jpg", raw, raw))
	}

	if len(contentIDs) > 0 {
		for _, cid := range contentIDs {
			addOneCID(cid)
		}
	} else {
		// 没有 r18 id，从番号原样猜（去掉连字符转小写）
		raw := strings.ToLower(strings.ReplaceAll(code, "-", ""))
		if raw != "" {
			addOneCID(raw)
		}
	}
	return urls
}

// downloadDMMPreviews 下载 DMM 剧照
// cid: content_id；variant: "digital" 或 "mono"
// max: 最多下载多少张；startIdx: 已成功数量（用于命名避免冲突）
// 遇到第一个失败就停
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
			break // 连续失败，停止
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
		if videoExts[ext] {
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