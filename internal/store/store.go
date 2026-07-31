package store

import "gorm.io/gorm"

type Store struct {
	DB *gorm.DB
}

// Open 使用给定 Dialer 打开数据库并完成通用初始化
// （AutoMigrate + seed routing_config 单例行）。
// 驱动特定的初始化（PRAGMA、连接池等）由 Dialer 实现负责。
func Open(dialer Dialer, dsn string) (*Store, error) {
	db, err := dialer.Open(dsn)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &RequestLog{}, &Setting{}, &ModelGroup{}, &ModelGroupItem{}); err != nil {
		return nil, err
	}
	// seed routing_config singleton row
	if err := db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1}).Error; err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}
