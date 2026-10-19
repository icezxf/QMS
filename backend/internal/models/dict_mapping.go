package models

import "time"

type ActorMapping struct {
	ID        uint      `gorm:"primarykey"`
	JpName    string    `gorm:"size:191;index"`
	ZhName    string    `gorm:"size:191"`
	ZhTwName  string    `gorm:"size:191"`
	Keyword   string    `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (ActorMapping) TableName() string {
	return "actor_mappings"
}

type InfoMapping struct {
	ID        uint      `gorm:"primarykey"`
	Category  string    `gorm:"size:50;index"`
	JpName    string    `gorm:"size:191;index"`
	ZhName    string    `gorm:"size:191"`
	Keyword   string    `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (InfoMapping) TableName() string {
	return "info_mappings"
}

type CNumberMapping struct {
	ID        uint      `gorm:"primarykey"`
	Number    string    `gorm:"size:50;uniqueIndex"`
	Title     string    `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (CNumberMapping) TableName() string {
	return "c_number_mappings"
}