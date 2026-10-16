package actordb

import (
	"time"

	"gorm.io/gorm"
)

// EmbyActorConfig Emby 代理单行配置
type EmbyActorConfig struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	EmbyURL    string    `gorm:"size:256" json:"emby_url"`
	EmbyAPIKey string    `gorm:"size:191" json:"emby_api_key"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (EmbyActorConfig) TableName() string { return "emby_proxy_configs" }

// Migrate 建表 + 初始化单行配置
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&EmbyActorConfig{}); err != nil {
		return err
	}
	var cnt int64
	db.Model(&EmbyActorConfig{}).Count(&cnt)
	if cnt == 0 {
		db.Create(&EmbyActorConfig{ID: 1})
	}
	return nil
}