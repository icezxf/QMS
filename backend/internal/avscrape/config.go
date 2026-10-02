package avscrape

import (
	"encoding/json"
	"errors"

	"qmediasync/internal/models"

	"gorm.io/gorm"
)

type Config struct {
	EnableMetaTube       bool   `json:"enable_metatube"`
	MetaTubeServer       string `json:"metatube_server"`
	EnableJavStash       bool   `json:"enable_javstash"`
	JavStashEndpoint     string `json:"javstash_endpoint"`
	JavStashAPIKey       string `json:"javstash_api_key"`
	EnableTranslate      bool   `json:"enable_translate"`
	TranslateEngine      string `json:"translate_engine"`
	TranslateTarget      string `json:"translate_target"`
	TranslateDeepLKey    string `json:"translate_deepl_key"`
	TranslateBingKey     string `json:"translate_bing_key"`
	TranslateBingRegion  string `json:"translate_bing_region"`
	TranslateGeminiKey   string `json:"translate_gemini_key"`
	TranslateGeminiModel string `json:"translate_gemini_model"`
	EnableJavDBRating    bool   `json:"enable_javdb_rating"`
	JavDBCookie          string `json:"javdb_cookie"`
	PreferChineseSource  bool   `json:"prefer_chinese_source"`

	EnableOshashMatch bool `json:"enable_oshash_match"`

	Watermark4K bool `json:"watermark_4k"`
	Watermark5K bool `json:"watermark_5k"`
	Watermark6K bool `json:"watermark_6k"`
	Watermark7K bool `json:"watermark_7k"`
	Watermark8K bool `json:"watermark_8k"`

	WatermarkSubtitle   bool `json:"watermark_subtitle"`
	WatermarkCrack      bool `json:"watermark_crack"`
	WatermarkLeak       bool `json:"watermark_leak"`
	WatermarkUncensored bool `json:"watermark_uncensored"`

	ExtraTagResolution bool `json:"extra_tag_resolution"`
	ExtraTagUncensored bool `json:"extra_tag_uncensored"`
	ExtraTagChineseSub bool `json:"extra_tag_chinese_sub"`

	// ===== Google Cloud Translation API（用于翻译特殊日文术语，如"ギリモザ"）=====
	GoogleTranslateAPIKey  string `json:"google_translate_api_key"`
	GoogleTranslateForTags bool   `json:"google_translate_for_tags"` // 仅标签走 Google
	GoogleTranslateForAll  bool   `json:"google_translate_for_all"`  // 全部走 Google
	// ==============================================================================
}

var defaultConfig = Config{
	EnableMetaTube:       true,
	MetaTubeServer:       "https://metatube-server.hf.space",
	EnableJavStash:       false,
	JavStashEndpoint:     "https://javstash.org/graphql",
	EnableTranslate:      false,
	TranslateEngine:      "gemini",
	TranslateTarget:      "zh",
	TranslateGeminiModel: "gemini-3.8-flash",
	EnableJavDBRating:    false,
	PreferChineseSource:  true,

	EnableOshashMatch: false,

	Watermark4K: true,
	Watermark5K: true,
	Watermark6K: true,
	Watermark7K: true,
	Watermark8K: true,

	WatermarkSubtitle:   true,
	WatermarkCrack:      true,
	WatermarkLeak:       true,
	WatermarkUncensored: true,

	ExtraTagResolution: true,
	ExtraTagUncensored: true,
	ExtraTagChineseSub: true,
}

func LoadConfig(db *gorm.DB) (*Config, error) {
	cfg := defaultConfig
	var row models.AVSettings
	err := db.Where("key = ?", "config").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(row.Value), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(db *gorm.DB, cfg *Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var row models.AVSettings
	err = db.Where("key = ?", "config").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(&models.AVSettings{Key: "config", Value: string(data)}).Error
	}
	if err != nil {
		return err
	}
	row.Value = string(data)
	return db.Save(&row).Error
}
