package aven

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"qmediasync/internal/avscrape"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
)

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

	// 用临时 AVPath 创建文件系统（复用 avscrape 的 FS）
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

	// 遍历视频
	videoFiles, err := walkVideosEN(fs, path.SourcePath, 0)
	if err != nil {
		return fmt.Errorf("遍历目录失败: %w", err)
	}
	helpers.AppLogger.Infof("[欧美扫描] 目录 %s 共找到 %d 个视频文件", path.SourcePath, len(videoFiles))

	for _, videoPath := range videoFiles {
		if err := s.processVideo(fs, &path, videoPath); err != nil {
			helpers.AppLogger.Warnf("[欧美扫描] %s 处理失败: %v", videoPath, err)
			continue
		}
	}

	s.DB.Model(&path).Update("last_scan_at", time.Now())
	return nil
}

func (s *ScannerEN) processVideo(fs avscrape.FileSystem, path *models.AVENPath, videoPath string) error {
	fileName := filepath.Base(videoPath)
	helpers.AppLogger.Infof("[欧美扫描] 处理: %s", fileName)

	// 1. 拿直链
	videoURL, err := fs.GetURL(videoPath)
	if err != nil {
		return fmt.Errorf("获取直链失败: %w", err)
	}

	// 2. 探测 osHash + 刮削
	headers := map[string]string{}
	result, oshash, err := s.Svc.ScrapeByURL(videoURL, headers)
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

	// 3. 检查是否已存在
	var existing models.AVENMedia
	if err := s.DB.Where("oshash = ?", oshash).First(&existing).Error; err == nil {
		if existing.Status == "paused" {
			helpers.AppLogger.Infof("[欧美扫描] %s 处于暂停状态，跳过", oshash)
			return nil
		}
	}

	// 4. 生成元数据
	baseName := sanitizePathN(result.Title)
	if baseName == "" {
		baseName = sanitizePathN(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	}
	files, prepWarnings, err := PrepareMetaFilesN(baseName, result, s.Svc.Config)
	allWarnings := append([]string{}, result.Warnings...)
	allWarnings = append(allWarnings, prepWarnings...)
	if err != nil {
		s.pauseMedia(baseName, videoPath, oshash, fmt.Sprintf("生成元数据失败: %v", err), allWarnings)
		return err
	}

	// 5. 检查完整性
	if missing := checkMetadataMissingEN(result); missing != "" {
		reason := fmt.Sprintf("元数据缺失: %s", missing)
		s.pauseMedia(baseName, videoPath, oshash, reason, allWarnings)
		return fmt.Errorf("%s", reason)
	}

	// 6. 整理视频
	media := MediaFromResultN(result, oshash, result.FileSize)
	if media == nil {
		return fmt.Errorf("构造 media 失败")
	}
	targetDir, err := OrganizeN(fs, path, media, []string{videoPath}, result)
	if err != nil {
		s.pauseMedia(baseName, videoPath, oshash, fmt.Sprintf("整理失败: %v", err), allWarnings)
		return err
	}

	// 7. 上传
	if len(files) > 0 {
		if _, err := fs.QueueUploads(files, targetDir, path.AccountID, path.SourceType); err != nil {
			s.pauseMedia(baseName, videoPath, oshash, fmt.Sprintf("加入上传队列失败: %v", err), allWarnings)
			return err
		}
	}

	// 8. 保存到数据库
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

// checkMetadataMissingEN 欧美完整性检查
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
	videoExts := map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
		".mov": true, ".flv": true, ".ts": true, ".m2ts": true,
		".iso": true, ".rmvb": true, ".strm": true,
	}
	for _, e := range entries {
		if e.IsDir {
			sub, err := walkVideosEN(fs, e.Path, depth+1)
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

// 用文件名兜底做一个假 oshash（仅用于记录日志）
func computeOshashFromName(name string) string {
	return "nohash-" + sanitizePathN(name)
}