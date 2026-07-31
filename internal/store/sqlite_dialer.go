package store

import (
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQLiteDialer 使用纯 Go 驱动 github.com/glebarez/sqlite 打开 SQLite 数据库。
// dsn 即 SQLite 文件路径（":memory:" 表示内存库）。
type SQLiteDialer struct{}

func (SQLiteDialer) Open(path string) (*gorm.DB, error) {
	// Ensure the parent directory exists for file-backed databases. Skipped
	// for ":memory:" (no directory) and for relative paths without a dir.
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
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
	return db, nil
}
