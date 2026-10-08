package models

import (
	"time"

	"qmediasync/internal/db"
)

// ===== 欧美刮削配置（键值对） =====
type AVENSettings struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

func (AVENSettings) TableName() string { return "aven_settings" }

// ===== 欧美刮削任务记录 =====
type AVENTask struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MediaId   uint      `gorm:"index" json:"media_id"`
	Oshash    string    `gorm:"index;size:64" json:"oshash"`
	StashID   string    `gorm:"index;size:64" json:"stash_id"`
	FilePath  string    `gorm:"type:text" json:"file_path"`
	Status    string    `gorm:"size:32" json:"status"` // pending/scraping/paused/done/failed/cancelled
	Message   string    `gorm:"type:text" json:"message"`
	Warnings  string    `gorm:"type:text" json:"warnings"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AVENTask) TableName() string { return "aven_tasks" }

// ===== 欧美刮削媒体库 =====
type AVENMedia struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	StashID       string `gorm:"index;size:64" json:"stash_id"` // StashDB scene id
	Oshash        string `gorm:"index;size:64" json:"oshash"`   // osHash（索引）

	Title         string `gorm:"type:text" json:"title"`
	OriginalTitle string `gorm:"type:text" json:"original_title"`
	Plot          string `gorm:"type:text" json:"plot"`          // 翻译后
	PlotOriginal  string `gorm:"type:text" json:"plot_original"` // 原文（英文）
	Runtime       int    `json:"runtime"`
	ReleaseDate   string `gorm:"size:32" json:"release_date"`
	Studio        string `gorm:"size:255" json:"studio"`
	StudioImage   string `gorm:"type:text" json:"studio_image"`
	Series        string `gorm:"size:255" json:"series"`

	Genres        string `gorm:"type:text" json:"genres"`         // JSON 数组
	Actors        string `gorm:"type:text" json:"actors"`         // JSON 数组
	Poster        string `gorm:"type:text" json:"poster"`
	Fanart        string `gorm:"type:text" json:"fanart"`
	PreviewImages string `gorm:"type:text" json:"preview_images"` // JSON 数组
	Trailer       string `gorm:"type:text" json:"trailer"`
	Rating        float64 `json:"rating"`
	Urls          string `gorm:"type:text" json:"urls"` // JSON 数组
	NFOContent    string `gorm:"type:text" json:"nfo_content"`
	Source        string `gorm:"size:32" json:"source"` // "stashdb"

	// 本地探测
	Resolution string `gorm:"size:32" json:"resolution"`
	IsHDR      bool   `json:"is_hdr"`
	FileSize   int64  `json:"file_size"`

	// 状态字段（和日本 AV 一致）
	Status        string    `gorm:"size:32;index;default:'pending'" json:"status"`
	ProgressStage string    `gorm:"size:64" json:"progress_stage"`
	PauseReason   string    `gorm:"type:text" json:"pause_reason"`
	PausedAt      time.Time `json:"paused_at"`
	TargetPath    string    `gorm:"type:text" json:"target_path"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AVENMedia) TableName() string { return "aven_media" }

// ===== 欧美刮削目录配置 =====
type AVENPath struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:128" json:"name"`
	SourceType   string    `gorm:"size:32" json:"source_type"` // 115 / openlist / local
	AccountID    uint      `json:"account_id"`
	SourcePath   string    `gorm:"type:text" json:"source_path"`
	TargetPath   string    `gorm:"type:text" json:"target_path"`
	Mode         string    `gorm:"size:32" json:"mode"`        // scrape_only / scrape_and_rename / rename_only
	MoveMethod   string    `gorm:"size:32" json:"move_method"` // move / copy
	NameTemplate string    `gorm:"size:255" json:"name_template"`
	Enable       bool      `json:"enable"`
	LastScanAt   time.Time `json:"last_scan_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (AVENPath) TableName() string { return "aven_paths" }

// ===== 辅助查询函数 =====

func GetAVENPathByID(id uint) *AVENPath {
	var p AVENPath
	if err := db.Db.First(&p, id).Error; err != nil {
		return nil
	}
	return &p
}

func GetAVENMediaByID(id uint) *AVENMedia {
	var m AVENMedia
	if err := db.Db.First(&m, id).Error; err != nil {
		return nil
	}
	return &m
}

func GetAVENMediaByOshash(oshash string) *AVENMedia {
	if oshash == "" {
		return nil
	}
	var m AVENMedia
	if err := db.Db.Where("oshash = ?", oshash).First(&m).Error; err != nil {
		return nil
	}
	return &m
}

func GetAVENMediaByStashID(stashID string) *AVENMedia {
	if stashID == "" {
		return nil
	}
	var m AVENMedia
	if err := db.Db.Where("stash_id = ?", stashID).First(&m).Error; err != nil {
		return nil
	}
	return &m
}