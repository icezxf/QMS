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
	DB *gorm.DB
}

func NewServiceEN(db *gorm.DB) *ServiceEN {
	return &ServiceEN{DB: db}
}

// ===== 懒加载：db 为 nil 时返回默认配置，避免测试崩溃 =====
func (s *ServiceEN) GetConfig() *Config {
	if s.DB == nil {
		def := defaultConfig
		return &def
	}
	cfg, err := LoadConfig(s.DB)
	if err != nil || cfg == nil {
		def := defaultConfig
		return &def
	}
	return cfg
}

func (s *ServiceEN) GetStashDB() *StashDBClient {
	cfg := s.GetConfig()
	if !cfg.EnableStashDB || cfg.StashDBAPIKey == "" {
		return nil
	}
	return NewStashDBClient(cfg.StashDBEndpoint, cfg.StashDBAPIKey)
}

// GetTPDB 获取 TPDB 客户端（未配置或未启用返回 nil）
func (s *ServiceEN) GetTPDB() *TPDBClient {
	cfg := s.GetConfig()
	if !cfg.EnableTPDB || cfg.TPDBAPIKey == "" {
		return nil
	}
	return NewTPDBClient(cfg.TPDBEndpoint, cfg.TPDBAPIKey)
}
// ==========================================================

// ScrapeByURL 通过视频 URL 刮削
//
// headers 由调用方（scanner_en.go）从 FS 层缓存里取好传进来，
// 内含 115 / OpenList 的 UA，不带会导致直链下载 403。
func (s *ServiceEN) ScrapeByURL(videoURL string, headers map[string]string) (*avscrape.ScrapeResult, string, error) {
	stashDB := s.GetStashDB()
	if stashDB == nil {
		return nil, "", fmt.Errorf("StashDB 未配置或 API Key 为空")
	}

	cfg := s.GetConfig()

	// 1. 探测 osHash + 分辨率/HDR
	pr, err := ProbeVideoByURL(videoURL, headers)
	if err != nil {
		return nil, "", fmt.Errorf("探测失败: %w", err)
	}
	if pr.Oshash == "" {
		return nil, "", fmt.Errorf("osHash 为空（文件可能太小）")
	}

	// 2. 查 StashDB（主匹配源）
	scene, err := stashDB.FindSceneByOshash(pr.Oshash)
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
	r.ExtraTags = buildExtraTagsN(r, cfg)

	// 3.5 强制用 scene.Images 覆盖 fanart 候选
	//     关键：SceneToResult 可能把 studio logo / performer 头像也塞进去了，
	//     这里必须清空重填，只用 scene 级图片。
	r.ImageCandidates = nil
	for _, img := range scene.Images {
		if img.URL != "" {
			r.ImageCandidates = append(r.ImageCandidates, img.URL)
		}
	}
	helpers.AppLogger.Infof("[欧美] fanart 候选图片 %d 张", len(r.ImageCandidates))
	for i, u := range r.ImageCandidates {
		helpers.AppLogger.Infof("[欧美]   [%d] %s", i, u)
	}

	var warnings []string

	// 4. 查 TPDB 补充 poster / rating / plot / studio（可选）
	if tpdb := s.GetTPDB(); tpdb != nil {
		tpdbScene, tpdbErr := tpdb.FindSceneByOshash(pr.Oshash)
		if tpdbErr != nil {
			helpers.AppLogger.Warnf("[TPDB] 查询失败: %v", tpdbErr)
			warnings = append(warnings, fmt.Sprintf("TPDB 查询失败: %v", tpdbErr))
		} else if tpdbScene != nil {
			// poster 优先用 TPDB 的 800x1200 竖版（JPEG 优先，避免 WebP 解码失败）
			if tpdbScene.Posters.Large != "" {
				r.Poster = tpdbScene.Posters.Large
				helpers.AppLogger.Infof("[TPDB] 补充 poster(large): %s", redactURL(tpdbScene.Posters.Large))
			} else if tpdbScene.Posters.Full != "" {
				r.Poster = tpdbScene.Posters.Full
				helpers.AppLogger.Infof("[TPDB] 补充 poster(full): %s", redactURL(tpdbScene.Posters.Full))
			}
			// rating
			if tpdbScene.Rating > 0 {
				r.Rating = tpdbScene.Rating
				helpers.AppLogger.Infof("[TPDB] 补充 rating: %.2f", tpdbScene.Rating)
			}
			// plot 兜底：StashDB 的 details 为空时用
			if r.Plot == "" {
				if plot := tpdbScene.GetPlot(); plot != "" {
					r.Plot = plot
					helpers.AppLogger.Infof("[TPDB] 补充 plot: %s", truncate(plot, 50))
				}
			}
			// studio 兜底
			if r.Studio == "" {
				if studio := tpdbScene.GetStudio(); studio != "" {
					r.Studio = studio
					helpers.AppLogger.Infof("[TPDB] 补充 studio: %s", studio)
				}
			}
		} else {
			helpers.AppLogger.Infof("[TPDB] oshash %s 未命中", pr.Oshash)
			warnings = append(warnings, "TPDB 未命中")
		}
	} else {
		helpers.AppLogger.Infof("[TPDB] 未配置或未启用，跳过补充")
	}

	// 5. 翻译（只译标题/剧情/标签）
	if cfg.EnableTranslate {
		tr := avscrape.NewTranslator(cfg.TranslateEngine, cfg.TranslateTarget)
		tr.SourceLang = "en"
		tr.DeepLKey = cfg.TranslateDeepLKey
		tr.BingKey = cfg.TranslateBingKey
		tr.BingRegion = cfg.TranslateBingRegion
		tr.GeminiKey = cfg.TranslateGeminiKey
		tr.GeminiModel = cfg.TranslateGeminiModel
		tr.GoogleAPIKey = cfg.GoogleTranslateAPIKey
		tr.GoogleTranslateForTags = cfg.GoogleTranslateForTags
		tr.GoogleTranslateForAll = cfg.GoogleTranslateForAll

		helpers.AppLogger.Infof("[欧美翻译] 开始翻译 (engine=%s)", cfg.TranslateEngine)
		warnings = append(warnings, s.translateResultN(tr, r, cfg)...)
	}

	r.Warnings = warnings
	return r, pr.Oshash, nil
}

// translateResultN 欧美翻译
func (s *ServiceEN) translateResultN(tr *avscrape.Translator, r *avscrape.ScrapeResult, cfg *Config) []string {
	var warnings []string
	if r == nil {
		return warnings
	}

	// ===== Google 全量优先：直接切引擎 =====
	// 勾了"全部走 Google" + 有 Key 时，绕开 DeepL/Gemini，直接用 Google Cloud
	if cfg.GoogleTranslateForAll && cfg.GoogleTranslateAPIKey != "" {
		helpers.AppLogger.Infof("[欧美翻译] 切换到 Google Cloud 引擎（全量）")
		tr.Engine = "google_cloud"
	}
	// ==========================================

	r.PlotOriginal = r.Plot

	// 标题
	if cfg.TranslateTitle && r.Title != "" {
		if t, err := tr.Translate(r.Title); err == nil && t != "" && t != r.Title {
			old := r.Title
			r.Title = t
			helpers.AppLogger.Infof("[欧美翻译] 标题: %s -> %s", truncate(old, 30), truncate(t, 30))
		} else if err != nil {
			warnings = append(warnings,
				avscrape.TranslateErrorPrefix+fmt.Sprintf("标题翻译失败: %v", err))
		}
	}

	// 简介
	if cfg.TranslatePlot && r.Plot != "" {
		if t, err := tr.Translate(r.Plot); err == nil && t != "" {
			r.Plot = t
			helpers.AppLogger.Infof("[欧美翻译] 简介: %s", truncate(t, 50))
		} else if err != nil {
			warnings = append(warnings,
				avscrape.TranslateErrorPrefix+fmt.Sprintf("简介翻译失败: %v", err))
		}
	}

	// 标签：一旦失败就停止循环，避免刷屏
	if cfg.TranslateTags {
		for i, g := range r.Genres {
			if t, err := tr.Translate(g); err == nil && t != "" && t != g {
				r.Genres[i] = t
			} else if err != nil {
				warnings = append(warnings,
					avscrape.TranslateErrorPrefix+fmt.Sprintf("标签 %q 翻译失败: %v", g, err))
				break
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
		tags = append(tags, "无码")
	}
	if cfg.ExtraTagChineseSub && r.HasChineseSub {
		tags = append(tags, "中文字幕")
	}
	return tags
}

// ===== 暂停操作 =====

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

var _ = strings.TrimSpace