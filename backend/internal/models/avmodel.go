package models

import (
	"time"

	"qmediasync/internal/db"
)

type AVSettings struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

// AVTask AV 刮削任务记录
type AVTask struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MediaId   uint      `gorm:"index" json:"media_id"`
	Code      string    `gorm:"index;size:64" json:"code"`
	FilePath  string    `gorm:"type:text" json:"file_path"`
	Status    string    `gorm:"size:32" json:"status"`
	Provider  string    `gorm:"size:32" json:"provider"`
	Message   string    `gorm:"type:text" json:"message"`
	// ===== 新增：警告列表（JSON 数组字符串）=====
	Warnings  string    `gorm:"type:text" json:"warnings"`
	// =========================================
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AVMedia AV 刮削结果（用于媒体库展示）
type AVMedia struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Code          string    `gorm:"index;size:64" json:"code"`
	Title         string    `gorm:"type:text" json:"title"`
	OriginalTitle string    `gorm:"type:text" json:"original_title"`
	Plot          string    `gorm:"type:text" json:"plot"`
	Runtime       int       `json:"runtime"`
	ReleaseDate   string    `gorm:"size:32" json:"release_date"`
	Director      string    `gorm:"size:255" json:"director"`
	Studio        string    `gorm:"size:255" json:"studio"`
	Label         string    `gorm:"size:255" json:"label"`
	Series        string    `gorm:"size:255" json:"series"`
	Genres        string    `gorm:"type:text" json:"genres"`
	Actors        string    `gorm:"type:text" json:"actors"`
	Poster        string    `gorm:"type:text" json:"poster"`
	Fanart        string    `gorm:"type:text" json:"fanart"`
	PreviewImages string    `gorm:"type:text" json:"preview_images"`
	Trailer       string    `gorm:"type:text" json:"trailer"`
	Rating        float64   `json:"rating"`
	Urls          string    `gorm:"type:text" json:"urls"`
	NFOContent    string    `gorm:"type:text" json:"nfo_content"`
	Translated    bool      `json:"translated"`
	Source        string    `gorm:"size:32" json:"source"`
	Oshash        string    `gorm:"index;size:64" json:"oshash"`

	Resolution    string    `gorm:"size:16" json:"resolution"`   // ← 加这一行

	Status        string    `gorm:"size:32;index;default:'pending'" json:"status"`
	ProgressStage string    `gorm:"size:64" json:"progress_stage"`
	PauseReason   string    `gorm:"type:text" json:"pause_reason"`
	PausedAt      time.Time `json:"paused_at"`
	TargetPath    string    `gorm:"type:text" json:"target_path"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AVPath struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:128" json:"name"`
	SourceType   string    `gorm:"size:32" json:"source_type"`
	AccountID    uint      `json:"account_id"`
	SourcePath   string    `gorm:"type:text" json:"source_path"`
	TargetPath   string    `gorm:"type:text" json:"target_path"`
	Mode         string    `gorm:"size:32" json:"mode"`
	MoveMethod   string    `gorm:"size:32" json:"move_method"`
	NameTemplate string    `gorm:"size:255" json:"name_template"`
	Enable       bool      `json:"enable"`
	LastScanAt   time.Time `json:"last_scan_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func GetAVPathByID(id uint) *AVPath {
	var p AVPath
	if err := db.Db.First(&p, id).Error; err != nil {
		return nil
	}
	return &p
}

func GetAVMediaByID(id uint) *AVMedia {
	var m AVMedia
	if err := db.Db.First(&m, id).Error; err != nil {
		return nil
	}
	return &m
}
