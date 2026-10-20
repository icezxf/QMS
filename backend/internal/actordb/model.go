package actordb

import (
	"time"

	"gorm.io/gorm"
)

type EmbyActorConfig struct {
	ID               uint      `gorm:"primarykey" json:"id"`
	EmbyURL          string    `gorm:"size:256" json:"emby_url"`
	EmbyAPIKey       string    `gorm:"size:191" json:"emby_api_key"`
	StashDBEndpoint  string    `gorm:"size:256" json:"stashdb_endpoint"`
	StashDBApiKey    string    `gorm:"size:191" json:"stashdb_api_key"`
	JavStashEndpoint string    `gorm:"size:256" json:"javstash_endpoint"`
	JavStashApiKey   string    `gorm:"size:191" json:"javstash_api_key"`
	TpdbApiKey       string    `gorm:"size:191" json:"tpdb_api_key"`
	WikiEmail        string    `gorm:"size:191" json:"wiki_email"` // 维基百科联系邮箱
	DeepLApiKey      string    `gorm:"size:191" json:"deepl_api_key"` // ★ 新增
	UpdatedAt        time.Time `json:"updated_at"`
}

func (EmbyActorConfig) TableName() string { return "emby_proxy_configs" }

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