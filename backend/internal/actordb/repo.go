package actordb

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct {
	DB *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo { return &Repo{DB: db} }

// UpsertActorName 只插名字，已存在则跳过
func (r *Repo) UpsertActorName(name string) error {
	return r.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(&ActorProfile{Name: name, SyncStatus: SyncStatusNone}).Error
}

func (r *Repo) GetActorByID(id uint) (*ActorProfile, error) {
	var p ActorProfile
	if err := r.DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) ListActors(page, size int, search string) ([]*ActorProfile, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	q := r.DB.Model(&ActorProfile{})
	if s := strings.TrimSpace(search); s != "" {
		like := "%" + s + "%"
		q = q.Where("name LIKE ? OR aliases LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*ActorProfile
	err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

func (r *Repo) UpdateActor(id uint, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	return r.DB.Model(&ActorProfile{}).Where("id = ?", id).Updates(fields).Error
}

func (r *Repo) DeleteActor(id uint) error {
	return r.DB.Delete(&ActorProfile{}, id).Error
}

func (r *Repo) CountActors() (int64, error) {
	var n int64
	err := r.DB.Model(&ActorProfile{}).Count(&n).Error
	return n, err
}

func (r *Repo) GetEmbyConfig() (*EmbyActorConfig, error) {
	var c EmbyActorConfig
	if err := r.DB.First(&c, 1).Error; err != nil {
		c = EmbyActorConfig{ID: 1, SyncCron: "0 3 * * *"}
		if err := r.DB.Create(&c).Error; err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func (r *Repo) UpdateEmbyConfig(c *EmbyActorConfig) error {
	c.ID = 1
	c.UpdatedAt = time.Now()
	return r.DB.Save(c).Error
}
