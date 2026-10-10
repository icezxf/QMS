package actordb

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ActorProfile 演员主档
type ActorProfile struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	Name         string    `gorm:"uniqueIndex;size:191" json:"name"`
	Aliases      string    `gorm:"type:text" json:"aliases"`  // JSON 数组
	AvatarURL    string    `gorm:"size:512" json:"avatar_url"` // 本地头像路径
	Bio          string    `gorm:"type:text" json:"bio"`
	StashDBID    string    `gorm:"size:64;index" json:"stashdb_id"`
	EmbyPersonID string    `gorm:"size:64;index" json:"emby_person_id"`
	SyncStatus   string    `gorm:"size:32;default:'none'" json:"sync_status"`
	LastSyncAt   int64     `json:"last_sync_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (ActorProfile) TableName() string { return "actor_profiles" }

// EmbyActorConfig Emby 演员联动独立配置
type EmbyActorConfig struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	EmbyURL    string    `gorm:"size:256" json:"emby_url"`
	EmbyAPIKey string    `gorm:"size:191" json:"emby_api_key"`
	StashDBKey string    `gorm:"size:191" json:"stashdb_key"`
	SyncCron   string    `gorm:"size:64" json:"sync_cron"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (EmbyActorConfig) TableName() string { return "emby_actor_configs" }

const (
	SyncStatusNone   = "none"
	SyncStatusSynced = "synced"
	SyncStatusFailed = "failed"
)

// Migrate 建表
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&ActorProfile{}, &EmbyActorConfig{}); err != nil {
		return fmt.Errorf("actordb 建表失败: %w", err)
	}
	var cnt int64
	db.Model(&EmbyActorConfig{}).Count(&cnt)
	if cnt == 0 {
		db.Create(&EmbyActorConfig{ID: 1, SyncCron: "0 3 * * *"})
	}
	return nil
}
