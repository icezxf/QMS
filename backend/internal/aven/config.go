package aven

import (
	"encoding/json"
	"errors"

	"qmediasync/internal/models"

	"gorm.io/gorm"
)

type Config struct {
	// ===== StashDB =====
	EnableStashDB   bool   `json:"enable_stashdb"`
	StashDBEndpoint string `json:"stashdb_endpoint"`
	StashDBAPIKey   string `json:"stashdb_api_key"`

	// ===== TPDB（补充 poster + rating）=====
	EnableTPDB   bool   `json:"enable_tpdb"`
	TPDBEndpoint string `json:"tpdb_endpoint"`
	TPDBAPIKey   string `json:"tpdb_api_key"`

	// ===== 翻译 =====
	EnableTranslate      bool   `json:"enable_translate"`
	TranslateTitle       bool   `json:"translate_title"`
	TranslatePlot        bool   `json:"translate_plot"`
	TranslateTags        bool   `json:"translate_tags"`
	TranslateEngine      string `json:"translate_engine"`
	TranslateTarget      string `json:"translate_target"`
	TranslateDeepLKey    string `json:"translate_deepl_key"`
	TranslateBingKey     string `json:"translate_bing_key"`
	TranslateBingRegion  string `json:"translate_bing_region"`
	TranslateGeminiKey   string `json:"translate_gemini_key"`
	TranslateGeminiModel string `json:"translate_gemini_model"`

	// ===== Google Cloud Translation =====
	GoogleTranslateAPIKey  string `json:"google_translate_api_key"`
	GoogleTranslateForTags bool   `json:"google_translate_for_tags"`
	GoogleTranslateForAll  bool   `json:"google_translate_for_all"`

	// ===== 附加标签 =====
	ExtraTagResolution bool `json:"extra_tag_resolution"`
	ExtraTagUncensored bool `json:"extra_tag_uncensored"`
	ExtraTagChineseSub bool `json:"extra_tag_chinese_sub"`

	// ===== 水印 =====
	Watermark4K bool `json:"watermark_4k"`
	Watermark5K bool `json:"watermark_5k"`
	Watermark6K bool `json:"watermark_6k"`
	Watermark7K bool `json:"watermark_7k"`
	Watermark8K bool `json:"watermark_8k"`

	WatermarkSubtitle   bool `json:"watermark_subtitle"`
	WatermarkCrack      bool `json:"watermark_crack"`
	WatermarkLeak       bool `json:"watermark_leak"`
	WatermarkUncensored bool `json:"watermark_uncensored"`

	WatermarkWidthPercent int `json:"watermark_width_percent"`   // ← 新增
}

var defaultConfig = Config{
	EnableStashDB:   true,
	StashDBEndpoint: "https://stashdb.org/graphql",

	EnableTPDB:   true,
	TPDBEndpoint: "https://theporndb.net/graphql",

	EnableTranslate:      false,
	TranslateTitle:       true,
	TranslatePlot:        true,
	TranslateTags:        true,
	TranslateEngine:      "gemini",
	TranslateTarget:      "zh",
	TranslateGeminiModel: "gemini-3.8-flash",

	ExtraTagResolution: true,
	ExtraTagUncensored: true,
	ExtraTagChineseSub: true,

	Watermark4K: true,
	Watermark5K: true,
	Watermark6K: true,
	Watermark7K: true,
	Watermark8K: true,

	WatermarkSubtitle:   true,
	WatermarkCrack:      true,
	WatermarkLeak:       true,
	WatermarkUncensored: true,

	WatermarkWidthPercent: 15,
}

func LoadConfig(db *gorm.DB) (*Config, error) {
	cfg := defaultConfig
	var row models.AVENSettings
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
	var row models.AVENSettings
	err = db.Where("key = ?", "config").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(&models.AVENSettings{Key: "config", Value: string(data)}).Error
	}
	if err != nil {
		return err
	}
	row.Value = string(data)
	return db.Save(&row).Error
}
