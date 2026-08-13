package store

import (
	"testing"
	"time"

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
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", ServedModel: "gpt-4o", ServedProvider: "openai", PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30})
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", ServedModel: "gpt-4o", ServedProvider: "openai", PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10})
	s.CreateLog(&RequestLog{RoutedModel: "claude-3", ServedModel: "claude-3", ServedProvider: "anthropic", PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300})

	byModel, err := s.TokenStatsByModel()
	assert.NoError(t, err)
	assert.Len(t, byModel, 2)
	// 按 total_tokens 倒序:claude-3(300) 在前
	assert.Equal(t, "claude-3", byModel[0].Model)
	assert.Equal(t, "anthropic", byModel[0].Provider)
	assert.Equal(t, int64(300), byModel[0].TotalTokens)
	assert.Equal(t, "gpt-4o", byModel[1].Model)
	assert.Equal(t, "openai", byModel[1].Provider)
	assert.Equal(t, int64(40), byModel[1].TotalTokens)
	assert.Equal(t, int64(15), byModel[1].PromptTokens)
	assert.Equal(t, int64(25), byModel[1].CompletionTokens)
	assert.Equal(t, int64(2), byModel[1].Count)

	total, err := s.TokenStatsTotal()
	assert.NoError(t, err)
	assert.Equal(t, int64(340), total)
}

func TestDailyUsageStats(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	// 今天 openai gpt-4o 两条
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", ServedModel: "gpt-4o", ServedProvider: "openai", PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, CreatedAt: now})
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", ServedModel: "gpt-4o", ServedProvider: "openai", PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10, CreatedAt: now})
	// 昨天 anthropic claude-3 一条
	s.CreateLog(&RequestLog{RoutedModel: "claude-3", ServedModel: "claude-3", ServedProvider: "anthropic", PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300, CreatedAt: now.Add(-24 * time.Hour)})

	rows, err := s.DailyUsageStats("", "", 30)
	assert.NoError(t, err)
	assert.Len(t, rows, 2)

	// 按日期升序，昨天在前
	assert.Equal(t, int64(1), rows[0].RequestCount)
	assert.Equal(t, int64(300), rows[0].TotalTokens)
	assert.Equal(t, int64(2), rows[1].RequestCount)
	assert.Equal(t, int64(40), rows[1].TotalTokens)
	assert.Equal(t, int64(15), rows[1].PromptTokens)
	assert.Equal(t, int64(25), rows[1].CompletionTokens)

	// provider 过滤
	rows, err = s.DailyUsageStats("anthropic", "", 30)
	assert.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, int64(300), rows[0].TotalTokens)

	// model 过滤（SQLite 的 = 本身区分大小写）
	rows, err = s.DailyUsageStats("", "gpt-4o", 30)
	assert.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, int64(40), rows[0].TotalTokens)
}

func TestDailyUsageStatsByModel(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	// 同一天：openai gpt-4o 两条 + anthropic claude-3 一条
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", ServedModel: "gpt-4o", ServedProvider: "openai", PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, CreatedAt: now})
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", ServedModel: "gpt-4o", ServedProvider: "openai", PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10, CreatedAt: now})
	s.CreateLog(&RequestLog{RoutedModel: "claude-3", ServedModel: "claude-3", ServedProvider: "anthropic", PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300, CreatedAt: now})

	rows, err := s.DailyUsageStatsByModel("", "", 30)
	assert.NoError(t, err)
	assert.Len(t, rows, 2) // 同一天按模型拆成两行

	// date 必须是纯 YYYY-MM-DD（MySQL 下 DATE() 会经 time.Time 变 RFC3339，须用 DATE_FORMAT 规避）
	for _, r := range rows {
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, r.Date)
	}

	// 外层 Order date asc, total_tokens desc：claude-3(300) 在前
	assert.Equal(t, "claude-3", rows[0].Model)
	assert.Equal(t, "anthropic", rows[0].Provider)
	assert.Equal(t, int64(300), rows[0].TotalTokens)
	assert.Equal(t, int64(1), rows[0].RequestCount)
	assert.Equal(t, "gpt-4o", rows[1].Model)
	assert.Equal(t, "openai", rows[1].Provider)
	assert.Equal(t, int64(40), rows[1].TotalTokens)
	assert.Equal(t, int64(15), rows[1].PromptTokens)
	assert.Equal(t, int64(25), rows[1].CompletionTokens)
	assert.Equal(t, int64(2), rows[1].RequestCount)
}
