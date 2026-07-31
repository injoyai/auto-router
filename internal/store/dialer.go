package store

import "gorm.io/gorm"

// Dialer 封装特定数据库驱动的连接逻辑。
// 每个实现负责打开连接并完成驱动特定的初始化
// （如 SQLite 的 PRAGMA、MySQL 的连接池调优）。
type Dialer interface {
	Open(dsn string) (*gorm.DB, error)
}
