package aven

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"qmediasync/internal/avscrape"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
)

type ServiceEN struct {
	DB      *gorm.DB
	StashDB *StashDBClient
	Config  *Config
}

func NewServiceEN(db *gorm.DB) *ServiceEN {
	cfg, err := LoadConfig(db)
	if err != nil || cfg == nil {
		def := defaultConfig
		cfg = &def
	}
	var stashDB *StashDBClient
	if cfg.EnableStashDB && cfg.StashDBAPIKey != "" {
		stashDB = NewStashDBClient(cfg.StashDBEndpoint, cfg.StashDBAPIKey)
	}
	return &ServiceEN{
		DB:      db,
		StashDB: stashDB,
		Config:  cfg,
	}
}

// ScrapeByURL 通过视频 URL 刮削
// 返回 (ScrapeResult, oshash, error)
func (s *ServiceEN) ScrapeByURL(videoURL string, headers map[string]string) (*avscrape.ScrapeResult, string, error) {
	if s.StashDB == nil {
		return nil, "", fmt.Errorf("StashDB 未配置或 API Key 为空")
	}

	// 1. 探测 osHash + 分辨率/HDR
	pr, err := ProbeVideoByURL(videoURL, headers)
	if err != nil {
		return nil, "", fmt.Errorf("探测失败: %w", err)
	}
	if pr.Oshash == "" {
		return nil, "", fmt.Errorf("osHash 为空（文件可能太小）")
	}

	// 2. 查 StashDB
	scene, err := s.StashDB.FindSceneByOshash(pr.Oshash)
	if err != nil {
		return nil, pr.Oshash, fmt.Errorf("查询 StashDB 失败: %w", err)
	}
	if scene == nil {
		return nil, pr.Oshash, fmt.Errorf("StashDB 未找到匹配场景（oshash=%s）", pr.Oshash)
	}

	// 3. 转换
	r := SceneToResult(scene)
	r.Oshash = pr.Oshash
	r.FileSize = pr.FileSize
	r.Resolution = pr.Resolution
	r.IsHDR = pr.IsHDR
	r.ExtraTags = buildExtraTagsN(r, s.Config)

	// 4. 翻译
	var warnings []string
	if s.Config.EnableTranslate {
		tr := avscrape.NewTranslator(s.Config.TranslateEngine, s.Config.TranslateTarget)
		tr.SourceLang = "en"
		tr.DeepLKey = s.Config.TranslateDeepLKey
		tr.BingKey = s.Config.TranslateBingKey
		tr.BingRegion = s.Config.TranslateBingRegion
		tr.GeminiKey = s.Config.TranslateGeminiKey
		tr.GeminiModel = s.Config.TranslateGeminiModel
		tr.GoogleAPIKey = s.Config.GoogleTranslateAPIKey
		tr.GoogleTranslateForTags = s.Config.GoogleTranslateForTags
		tr.GoogleTranslateForAll = s.Config.GoogleTranslateForAll

		helpers.AppLogger.Infof("[欧美翻译] 开始翻译 (engine=%s)", s.Config.TranslateEngine)
		warnings = append(warnings, s.translateResultN(tr, r)...)
	}

	r.Warnings = warnings
	return r, pr.Oshash, nil
}

// translateResultN 欧美翻译：标题/简介/标签
func (s *ServiceEN) translateResultN(tr *avscrape.Translator, r *avscrape.ScrapeResult) []string {
	var warnings []string
	if r == nil {
		return warnings
	}

	// 保存简介原文
	r.PlotOriginal = r.Plot

	// 标题
	if s.Config.TranslateTitle && r.Title != "" {
		if t, err := tr.Translate(r.Title); err == nil && t != "" && t != r.Title {
			old := r.Title
			r.Title = t
			helpers.AppLogger.Infof("[欧美翻译] 标题: %s -> %s", truncate(old, 30), truncate(t, 30))
		} else if err != nil {
			warnings = append(warnings, fmt.Sprintf("标题翻译失败: %v", err))
		}
	}

	// 简介
	if s.Config.TranslatePlot && r.Plot != "" {
		if t, err := tr.Translate(r.Plot); err == nil && t != "" {
			r.Plot = t
			helpers.AppLogger.Infof("[欧美翻译] 简介: %s", truncate(t, 50))
		} else if err != nil {
			warnings = append(warnings, fmt.Sprintf("简介翻译失败: %v", err))
		}
	}

	// 标签
	if s.Config.TranslateTags {
		for i, g := range r.Genres {
			if t, err := tr.Translate(g); err == nil && t != "" && t != g {
				r.Genres[i] = t
			}
		}
	}

	return warnings
}

// buildExtraTagsN 欧美附加标签
func buildExtraTagsN(r *avscrape.ScrapeResult, cfg *Config) []string {
	var tags []string
	if cfg.ExtraTagResolution && r.Resolution != "" {
		tags = append(tags, r.Resolution)
		if r.IsHDR {
			tags = append(tags, "HDR")
		}
	}
	if cfg.ExtraTagUncensored {
		// 欧美默认无码
		tags = append(tags, "无码")
	}
	if cfg.ExtraTagChineseSub && r.HasChineseSub {
		tags = append(tags, "中文字幕")
	}
	return tags
}

// ============================================================
// 暂停操作
// ============================================================

func (s *ServiceEN) ReleaseMedia(id uint) error {
	media := models.GetAVENMediaByID(id)
	if media == nil {
		return fmt.Errorf("媒体记录不存在")
	}
	if media.Status != "paused" {
		return fmt.Errorf("当前状态为 %s，不是暂停状态，无法放行", media.Status)
	}
	if err := s.DB.Model(media).Updates(map[string]any{
		"status":       "released",
		"pause_reason": "",
	}).Error; err != nil {
		return err
	}
	helpers.AppLogger.Infof("[欧美放行] %s 已放行", media.Title)
	return nil
}

func (s *ServiceEN) RestartMedia(id uint) error {
	media := models.GetAVENMediaByID(id)
	if media == nil {
		return fmt.Errorf("媒体记录不存在")
	}
	tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "aven", media.Oshash)
	_ = os.RemoveAll(tmpDir)
	imgDir := filepath.Join(helpers.ConfigDir, "aven_images", media.Oshash)
	_ = os.RemoveAll(imgDir)
	if err := s.DB.Delete(&models.AVENMedia{}, id).Error; err != nil {
		return err
	}
	s.DB.Where("media_id = ?", id).Delete(&models.AVENTask{})
	helpers.AppLogger.Infof("[欧美重启] %s 已重启", media.Title)
	return nil
}

func (s *ServiceEN) CancelMedia(id uint) error {
	media := models.GetAVENMediaByID(id)
	if media == nil {
		return fmt.Errorf("媒体记录不存在")
	}
	tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "aven", media.Oshash)
	_ = os.RemoveAll(tmpDir)
	imgDir := filepath.Join(helpers.ConfigDir, "aven_images", media.Oshash)
	_ = os.RemoveAll(imgDir)
	if err := s.DB.Delete(&models.AVENMedia{}, id).Error; err != nil {
		return err
	}
	s.DB.Where("media_id = ?", id).Delete(&models.AVENTask{})
	helpers.AppLogger.Infof("[欧美取消] %s 已取消", media.Title)
	return nil
}

func (s *ServiceEN) BatchDeleteMedia(ids []uint) (int, error) {
	var mediaList []models.AVENMedia
	query := s.DB.Model(&models.AVENMedia{})
	if len(ids) > 0 {
		query = query.Where("id IN ?", ids)
	}
	if err := query.Find(&mediaList).Error; err != nil {
		return 0, err
	}
	if len(mediaList) == 0 {
		return 0, nil
	}

	mediaIDs := make([]uint, 0, len(mediaList))
	for _, m := range mediaList {
		mediaIDs = append(mediaIDs, m.ID)
		tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "aven", m.Oshash)
		_ = os.RemoveAll(tmpDir)
		imgDir := filepath.Join(helpers.ConfigDir, "aven_images", m.Oshash)
		_ = os.RemoveAll(imgDir)
	}

	if err := s.DB.Where("id IN ?", mediaIDs).Delete(&models.AVENMedia{}).Error; err != nil {
		return 0, err
	}
	s.DB.Where("media_id IN ?", mediaIDs).Delete(&models.AVENTask{})
	helpers.AppLogger.Infof("[欧美批量删除] 已删除 %d 条媒体记录", len(mediaIDs))
	return len(mediaIDs), nil
}

func (s *ServiceEN) GetMedia(id uint) (*models.AVENMedia, error) {
	var m models.AVENMedia
	err := s.DB.First(&m, id).Error
	return &m, err
}

// 保留占位，防止 unused
var _ = strings.TrimSpace