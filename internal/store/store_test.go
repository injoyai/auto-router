package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(SQLiteDialer{}, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestOpenAutoMigrates(t *testing.T) {
	s := newTestStore(t)
	err := s.DB.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &RequestLog{}, &Setting{}, &ModelGroup{}, &ModelGroupItem{})
	assert.NoError(t, err)

	// Tables exist and are queryable. Open() also seeds the routing_configs
	// singleton row (ID=1), so it has 1 row; all others are empty.
	var count int64
	emptyTables := []string{"providers", "models", "request_logs", "settings"}
	for _, tbl := range emptyTables {
		s.DB.Table(tbl).Count(&count)
		assert.Equal(t, int64(0), count, "table %s should exist and be empty", tbl)
	}
	s.DB.Table("routing_configs").Count(&count)
	assert.Equal(t, int64(1), count, "table routing_configs should contain the seeded singleton row")
}

func TestTokenAggregations(t *testing.T) {
	s := newTestStore(t)
	// 两条 gpt-4o,一条 claude-3
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30})
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10})
	s.CreateLog(&RequestLog{RoutedModel: "claude-3", PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300})

	byModel, err := s.TokenStatsByModel()
	assert.NoError(t, err)
	assert.Len(t, byModel, 2)
	// 按 total_tokens 倒序:claude-3(300) 在前
	assert.Equal(t, "claude-3", byModel[0].Model)
	assert.Equal(t, int64(300), byModel[0].TotalTokens)
	assert.Equal(t, "gpt-4o", byModel[1].Model)
	assert.Equal(t, int64(40), byModel[1].TotalTokens)
	assert.Equal(t, int64(15), byModel[1].PromptTokens)
	assert.Equal(t, int64(25), byModel[1].CompletionTokens)
	assert.Equal(t, int64(2), byModel[1].Count)

	total, err := s.TokenStatsTotal()
	assert.NoError(t, err)
	assert.Equal(t, int64(340), total)
}
