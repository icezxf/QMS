package actordb

import "gorm.io/gorm"

// Init 建表 + 返回 handler
func Init(db *gorm.DB) (*Handler, error) {
	if err := Migrate(db); err != nil {
		return nil, err
	}
	return NewHandler(NewRepo(db)), nil
}