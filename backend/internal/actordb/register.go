package actordb

import (
	"qmediasync/internal/models"

	"gorm.io/gorm"
)

// Init 主程序启动时调用
func Init(db *gorm.DB) (*Handler, error) {
	if err := Migrate(db); err != nil {
		return nil, err
	}
	repo := NewRepo(db)
	h := NewHandler(repo)

	h.LoadAV = func() ([]string, error) {
		var rows []models.AVMedia
		if err := db.Select("actors").Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Actors)
		}
		return out, nil
	}

	h.LoadAVEN = func() ([]string, error) {
		var rows []models.AVENMedia
		if err := db.Select("actors").Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Actors)
		}
		return out, nil
	}

	return h, nil
}
