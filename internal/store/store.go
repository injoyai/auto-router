package store

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	DB *gorm.DB
}

func Open(path string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &Session{}, &RequestLog{}, &Setting{}); err != nil {
		return nil, err
	}
	// seed routing_config singleton row
	if err := db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1}).Error; err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}
