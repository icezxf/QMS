package actordb

import (
	"fmt"

	"gorm.io/gorm"
)

// Migrate 建表（幂等，不改已有表）
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&ActorProfile{},
		&ActorWork{},
		&ActorSyncTask{},
		&EmbyActorConfig{},
	); err != nil {
		return fmt.Errorf("actordb 建表失败: %w", err)
	}
	// 初始化默认配置行（ID=1）
	var cnt int64
	db.Model(&EmbyActorConfig{}).Count(&cnt)
	if cnt == 0 {
		db.Create(&EmbyActorConfig{ID: 1, SyncCron: "0 3 * * *"})
	}
	return nil
}
