package actordb

import (
	"time"

	"gorm.io/gorm"
)

type Repo struct{ DB *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{DB: db} }

func (r *Repo) GetConfig() (*EmbyActorConfig, error) {
	var c EmbyActorConfig
	if err := r.DB.First(&c, 1).Error; err != nil {
		c = EmbyActorConfig{ID: 1}
		if err := r.DB.Create(&c).Error; err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func (r *Repo) UpdateConfig(c *EmbyActorConfig) error {
	c.ID = 1
	c.UpdatedAt = time.Now()
	return r.DB.Save(c).Error
}