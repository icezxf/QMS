package aven

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/avscrape"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
)

// ===== 扫描过滤器：只处理指定 oshash =====
var (
	scanFilterNMu       sync.Mutex
	scanFilterNOshashes map[string]bool
)

func SetScanFilterN(oshashes []string) {
	scanFilterNMu.Lock()
	defer scanFilterNMu.Unlock()
	if len(oshashes) == 0 {
		scanFilterNOshashes = nil
		return
	}
	scanFilterNOshashes = make(map[string]bool, len(oshashes))
	for _, c := range oshashes {
		scanFilterNOshashes[c] = true
	}
}

// takeScanFilterN 取出 filter 快照并立即清空
// 关键：filter 只在本次扫描生效，不污染其他并发扫描
func takeScanFilterN() map[string]bool {
	scanFilterNMu.Lock()
	defer scanFilterNMu.Unlock()
	if len(scanFilterNOshashes) == 0 {
		return nil
	}
	cp := make(map[string]bool, len(scanFilterNOshashes))
	for k, v := range scanFilterNOshashes {
		cp[k] = v
	}
	scanFilterNOshashes = nil
	return cp
}

func clearScanFilterN() {
	scanFilterNMu.Lock()
	defer scanFilterNMu.Unlock()
	scanFilterNOshashes = nil
}
// ==========================================

// ===== 源目录清理：没有 >1G 视频的子目录整个删除 =====
const (
	cleanupBigVideoSizeEN      = int64(1) << 30
	cleanupDeleteIntervalEN    = 500 * time.Millisecond
	cleanupMaxDeletesPerScanEN = 100
)

var videoExtsN = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
	".mov": true, ".flv": true, ".ts": true, ".m2ts": true,
	".iso": true, ".rmvb": true, ".strm": true,
}

func cleanupSourceDirN(fs avscrape.FileSystem, srcDir string) {
	entries, err := fs.ListDetailed(srcDir)
	if err != nil {
		helpers.AppLogger.Warnf("[欧美清理] 列出源目录失败 %s: %v", srcDir, err)
		return
	}

	deleted := 0
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		cleanupDirRecursiveN(fs, e.Path, &deleted)
	}

	if deleted > 0 {
		helpers.AppLogger.Infof("[欧美清理] 本次共删除 %d 个残留目录", deleted)
	}
}

func cleanupDirRecursiveN(fs avscrape.FileSystem, dir string, deleted *int) bool {
	entries, err := fs.ListDetailed(dir)
	if err != nil {
		return false
	}

	hasBigVideo := false

	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		subRemoved := cleanupDirRecursiveN(fs, e.Path, deleted)
		if !subRemoved {
			hasBigVideo = true
		}
	}

	if !hasBigVideo {
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			if !isVideoNameN(e.Name) {
				continue
			}
			if e.Size > cleanupBigVideoSizeEN {
				hasBigVideo = true
				break
			}
		}
	}

	if hasBigVideo {
		return false
	}

	if *deleted >= cleanupMaxDeletesPerScanEN {
		helpers.AppLogger.Warnf("[欧美清理] 已达到单次删除上限 %d，跳过 %s",
			cleanupMaxDeletesPerScanEN, dir)
		return false
	}

	if *deleted > 0 && cleanupDeleteIntervalEN > 0 {
		time.Sleep(cleanupDeleteIntervalEN)
	}

	if err := fs.DeleteDir(dir); err != nil {
		helpers.AppLogger.Warnf("[欧美清理] 删除目录失败 %s: %v", dir, err)
		return false
	}
	*deleted++
	helpers.AppLogger.Infof("[欧美清理] 已删除残留目录: %s", dir)
	return true
}

func isVideoNameN(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExtsN[ext]
}
// ==================================================

type ScannerEN struct {
	DB  *gorm.DB
	Svc *ServiceEN
}

func NewScannerEN(db *gorm.DB) *ScannerEN {
	return &ScannerEN{DB: db, Svc: NewServiceEN(db)}
}

func (s *ScannerEN) Scan(pathID uint) error {
	var path models.AVENPath
	if err := s.DB.First(&path, pathID).Error; err != nil {
		return err
	}
	if !path.Enable {
		return fmt.Errorf("目录未启用: %d", pathID)
	}

	// ===== 扫描开始时取出 filter 快照并立即清空 =====
	// 之后即使 SetScanFilterN 被调用（用户点重启/放行），也不影响本次扫描
	filterSnapshot := takeScanFilterN()
	if len(filterSnapshot) > 0 {
		helpers.AppLogger.Infof("[欧美扫描] 本次仅处理 %d 个 oshash", len(filterSnapshot))
	}
	// =================================================

	tempAVPath := &models.AVPath{
		ID:           path.ID,
		SourceType:   path.SourceType,
		AccountID:    path.AccountID,
		SourcePath:   path.SourcePath,
		TargetPath:   path.TargetPath,
		Mode:         path.Mode,
		MoveMethod:   path.MoveMethod,
		NameTemplate: path.NameTemplate,
		Enable:       path.Enable,
	}
	fs, err := avscrape.NewFileSystem(tempAVPath)
	if err != nil {
		return fmt.Errorf("创建文件系统失败: %w", err)
	}

	helpers.AppLogger.Infof("[欧美扫描] 开始清理源目录: %s", path.SourcePath)
	cleanupSourceDirN(fs, path.SourcePath)

	videoFiles, err := walkVideosEN(fs, path.SourcePath, 0)
	if err != nil {
		return fmt.Errorf("遍历目录失败: %w", err)
	}
	helpers.AppLogger.Infof("[欧美扫描] 目录 %s 共找到 %d 个视频文件", path.SourcePath, len(videoFiles))

	for _, videoPath := range videoFiles {
		if err := s.processVideo(fs, &path, videoPath, filterSnapshot); err != nil {
			helpers.AppLogger.Warnf("[欧美扫描] %s 处理失败: %v", videoPath, err)
			continue
		}
	}

	helpers.AppLogger.Infof("[欧美扫描] 扫描结束，二次清理源目录: %s", path.SourcePath)
	cleanupSourceDirN(fs, path.SourcePath)

	s.DB.Model(&path).Update("last_scan_at", time.Now())
	return nil
}

func (s *ScannerEN) processVideo(fs avscrape.FileSystem, path *models.AVENPath, videoPath string, filterSnapshot map[string]bool) error {
	fileName := filepath.Base(videoPath)
	helpers.AppLogger.Infof("[欧美扫描] 处理: %s", fileName)

	// 1. 拿直链
	videoURL, err := fs.GetURL(videoPath)
	if err != nil {
		return fmt.Errorf("获取直链失败: %w", err)
	}

	// 2. 从 avscrape 缓存取 header（含 115 UA）
	headers := map[string]string{}
	if h := avscrape.GetURLHeader(videoURL); h != nil {
		for k, vs := range h {
			for _, v := range vs {
				headers[k] = v
			}
		}
	}

	// 3. 探测 + 刮削（带 filter 快照，提前过滤）
	result, oshash, err := s.Svc.ScrapeByURL(videoURL, headers, filterSnapshot)

	// 3.5 filter 命中 → 静默跳过（此时只有探测，没有查 StashDB/TPDB/翻译）
	if err == ErrFilteredOut {
		helpers.AppLogger.Infof("[欧美扫描] %s 不在本次过滤范围内，跳过", oshash)
		return nil
	}

	if err != nil {
		if oshash == "" {
			oshash = computeOshashFromName(fileName)
		}
		s.pauseMedia("", videoPath, oshash, err.Error(), nil)
		return err
	}

	if result == nil {
		return fmt.Errorf("刮削结果为空")
	}

	// 4. 提前算 baseName
	baseName := sanitizePathN(result.Title)
	if baseName == "" {
		baseName = sanitizePathN(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	}

	// 4.5 翻译错误 → 暂停
	if avscrape.HasTranslateError(result.Warnings) {
		reason := "翻译错误（详见警告列表）"
		s.pauseMedia(baseName, videoPath, oshash, reason, result.Warnings)
		helpers.AppLogger.Warnf("[欧美暂停] %s 翻译失败，已暂停", oshash)
		return fmt.Errorf("%s", reason)
	}

	// 5. 检查是否已存在
	var existing models.AVENMedia
	if err := s.DB.Where("oshash = ?", oshash).First(&existing).Error; err == nil {
		if existing.Status == "paused" {
			helpers.AppLogger.Infof("[欧美扫描] %s 处于暂停状态，跳过", oshash)
			return nil
		}
		if existing.Status == "completed" {
			helpers.AppLogger.Infof("[欧美扫描] %s 已完成，跳过", oshash)
			return nil
		}
	}

	// 6. 生成元数据
	files, prepWarnings, err := PrepareMetaFilesN(baseName, result, s.Svc.GetConfig())
	allWarnings := append([]string{}, result.Warnings...)
	allWarnings = append(allWarnings, prepWarnings...)
	if err != nil {
		s.pauseMedia(baseName, videoPath, oshash, fmt.Sprintf("生成元数据失败: %v", err), allWarnings)
		return err
	}

	// 7. 检查完整性
	if missing := checkMetadataMissingEN(result); missing != "" {
		reason := fmt.Sprintf("元数据缺失: %s", missing)
		s.pauseMedia(baseName, videoPath, oshash, reason, allWarnings)
		return fmt.Errorf("%s", reason)
	}

	// 8. 整理视频
	media := MediaFromResultN(result, oshash, result.FileSize)
	if media == nil {
		return fmt.Errorf("构造 media 失败")
	}
	targetDir, err := OrganizeN(fs, path, media, []string{videoPath}, result)
	if err != nil {
		s.pauseMedia(baseName, videoPath, oshash, fmt.Sprintf("整理失败: %v", err), allWarnings)
		return err
	}

	// 9. 上传
	if len(files) > 0 {
		if _, err := fs.QueueUploads(files, targetDir, path.AccountID, path.SourceType); err != nil {
			s.pauseMedia(baseName, videoPath, oshash, fmt.Sprintf("加入上传队列失败: %v", err), allWarnings)
			return err
		}
	}

	// 10. 保存到数据库
	s.saveMedia(result, oshash, targetDir, allWarnings)

	helpers.AppLogger.Infof("[欧美扫描] %s 刮削完成", fileName)
	return nil
}

func (s *ScannerEN) saveMedia(r *avscrape.ScrapeResult, oshash, targetPath string, warnings []string) {
	media := MediaFromResultN(r, oshash, r.FileSize)
	if media == nil {
		return
	}
	media.Status = "completed"
	media.TargetPath = targetPath

	var existing models.AVENMedia
	if err := s.DB.Where("oshash = ?", oshash).First(&existing).Error; err == nil {
		media.ID = existing.ID
		media.CreatedAt = existing.CreatedAt
		s.DB.Save(media)
		s.recordTask(media.ID, oshash, targetPath, "done", "刮削完成", warnings)
	} else {
		s.DB.Create(media)
		s.recordTask(media.ID, oshash, targetPath, "done", "刮削完成", warnings)
	}
}

func (s *ScannerEN) pauseMedia(code, filePath, oshash, reason string, warnings []string) {
	helpers.AppLogger.Warnf("[欧美暂停] %s: %s", oshash, reason)

	var media *models.AVENMedia
	if oshash != "" {
		media = models.GetAVENMediaByOshash(oshash)
	}
	if media == nil {
		media = &models.AVENMedia{
			Oshash:      oshash,
			Status:      "paused",
			PauseReason: reason,
			PausedAt:    time.Now(),
		}
		s.DB.Create(media)
	} else {
		s.DB.Model(media).Updates(map[string]any{
			"status":       "paused",
			"pause_reason": reason,
			"paused_at":    time.Now(),
		})
	}
	s.recordTask(media.ID, oshash, filePath, "paused", reason, warnings)
}

func (s *ScannerEN) recordTask(mediaID uint, oshash, filePath, status, msg string, warnings []string) {
	warningsJSON, _ := json.Marshal(warnings)
	s.DB.Create(&models.AVENTask{
		MediaId:  mediaID,
		Oshash:   oshash,
		FilePath: filePath,
		Status:   status,
		Message:  msg,
		Warnings: string(warningsJSON),
	})
}

func checkMetadataMissingEN(r *avscrape.ScrapeResult) string {
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
	if len(r.Actors) == 0 {
		missing = append(missing, "Actors")
	}
	return strings.Join(missing, ", ")
}

func walkVideosEN(fs avscrape.FileSystem, root string, depth int) ([]string, error) {
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
			sub, err := walkVideosEN(fs, e.Path, depth+1)
			if err == nil {
				videos = append(videos, sub...)
			}
			continue
		}
		if videoExtsN[strings.ToLower(filepath.Ext(e.Name))] {
			videos = append(videos, e.Path)
		}
	}
	return videos, nil
}

func computeOshashFromName(name string) string {
	return "nohash-" + sanitizePathN(name)
}
