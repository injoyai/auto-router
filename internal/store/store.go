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
	// I8: enable WAL + a 5s busy_timeout so concurrent readers/writers on
	// file-backed databases don't immediately fail with "database is locked".
	// These are no-ops on :memory: databases (per-connection, no journal) but
	// never error, so they are safe to apply unconditionally and before any
	// migration/write.
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
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
