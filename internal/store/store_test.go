package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestOpenAutoMigrates(t *testing.T) {
	s := newTestStore(t)
	err := s.DB.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &Session{}, &RequestLog{}, &Setting{})
	assert.NoError(t, err)

	// Tables exist and are queryable. Open() also seeds the routing_configs
	// singleton row (ID=1), so it has 1 row; all others are empty.
	var count int64
	emptyTables := []string{"providers", "models", "sessions", "request_logs", "settings"}
	for _, tbl := range emptyTables {
		s.DB.Table(tbl).Count(&count)
		assert.Equal(t, int64(0), count, "table %s should exist and be empty", tbl)
	}
	s.DB.Table("routing_configs").Count(&count)
	assert.Equal(t, int64(1), count, "table routing_configs should contain the seeded singleton row")
}
