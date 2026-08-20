package store

import (
	"time"

	"auto-router/internal/model"
)

// Attempt records one model attempt in the execution chain.
type Attempt struct {
	Type      string `json:"type,omitempty"` // "judge" for judge attempts, "" for model queue
	Model     string `json:"model"`
	Provider  string `json:"provider"`
	Success   bool   `json:"success"`
	Status    int    `json:"status"` // HTTP status code (0 = network error)
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}

type RequestLog struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	SessionID        string    `json:"session_id"`
	ClientProtocol   string    `json:"client_protocol"`
	RequestedModel   string    `json:"requested_model"`
	RoutedModel      string    `json:"routed_model"`
	RouteReason      string    `json:"route_reason"`
	JudgeRaw         string    `json:"judge_raw"`
	Status           int       `json:"status"`
	LatencyMs        int64     `json:"latency_ms"`
	Error            string    `json:"error"`
	RetryCount       int    `json:"retry_count"`
	ServedModel      string `json:"served_model"`   // actually served model name (queue = the successful one)
	ServedProvider   string `json:"served_provider"` // provider that actually served the request
	FailoverCount    int    `json:"failover_count"` // queue failover count
	Trace            string `json:"trace"`          // JSON array of Attempt, the full model queue attempt history
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CacheTokens      int       `json:"cache_tokens"`
	// Judge call diagnostics. Populated only when the judge model was invoked
	// (auto routing). Kept separate from the main token fields so that judge
	// overhead does not inflate the execution model's token stats.
	JudgeModel            string    `json:"judge_model"`
	JudgeLatencyMs        int64     `json:"judge_latency_ms"`
	JudgePromptTokens     int       `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int       `json:"judge_completion_tokens"`
	JudgeTotalTokens      int       `json:"judge_total_tokens"`
	JudgeCacheTokens      int       `json:"judge_cache_tokens"`
	CreatedAt             time.Time `json:"created_at"`
}

// LogWithProvider wraps RequestLog with a resolved provider name (via JOIN).
type LogWithProvider struct {
	RequestLog
	ProviderName string `gorm:"column:provider_name" json:"provider_name"`
}

func (s *Store) CreateLog(l *RequestLog) error {
	return s.DB.Create(l).Error
}

// UpdateLogTrace updates the trace field (and served_model/served_provider) of
// an existing log. Used for real-time progress: each attempt appends to the
// trace so the frontend can observe the chain growing before the request finishes.
func (s *Store) UpdateLogTrace(id uint, trace, servedModel, servedProvider string) error {
	return s.DB.Model(&RequestLog{}).Where("id = ?", id).
		Updates(map[string]any{"trace": trace, "served_model": servedModel, "served_provider": servedProvider}).Error
}

// UpdateLogFinal writes the final state of a request log (status, latency,
// error, retry count, token usage, judge diagnostics, served model, failover).
func (s *Store) UpdateLogFinal(id uint, status int, latencyMs int64, errMsg string, retryCount int, servedModel, servedProvider string, failoverCount int, usage *model.Usage, judgeRaw, judgeModel string, judgeLatencyMs int64, judgeUsage *model.Usage) error {
	updates := map[string]any{
		"status":           status,
		"latency_ms":       latencyMs,
		"error":            errMsg,
		"retry_count":      retryCount,
		"served_model":     servedModel,
		"served_provider":  servedProvider,
		"failover_count":   failoverCount,
		"judge_raw":        judgeRaw,
		"judge_model":      judgeModel,
		"judge_latency_ms": judgeLatencyMs,
	}
	if usage != nil {
		updates["prompt_tokens"] = usage.PromptTokens
		updates["completion_tokens"] = usage.CompletionTokens
		updates["total_tokens"] = usage.TotalTokens
		updates["cache_tokens"] = usage.CacheTokens
	}
	if judgeUsage != nil {
		updates["judge_prompt_tokens"] = judgeUsage.PromptTokens
		updates["judge_completion_tokens"] = judgeUsage.CompletionTokens
		updates["judge_total_tokens"] = judgeUsage.TotalTokens
		updates["judge_cache_tokens"] = judgeUsage.CacheTokens
	}
	return s.DB.Model(&RequestLog{}).Where("id = ?", id).Updates(updates).Error
}

// ClearLogs deletes all request log rows. SQLite/TRUNCATE 不兼容,用 DELETE 全表。
func (s *Store) ClearLogs() error {
	return s.DB.Where("1 = 1").Delete(&RequestLog{}).Error
}

func (s *Store) ListLogs(page, pageSize int, reason, model string) ([]LogWithProvider, int64, error) {
	var logs []LogWithProvider
	var total int64
	// 主查询：零 JOIN，避免同名模型导致重复行
	q := s.DB.Table("request_logs")
	if reason != "" {
		q = q.Where("request_logs.route_reason = ?", reason)
	}
	if model != "" {
		// MySQL 默认 collation 大小写不敏感，加 BINARY 强制区分；
		// SQLite 的 = 本身区分大小写，无需处理。
		if s.DB.Dialector.Name() == "mysql" {
			q = q.Where("BINARY request_logs.routed_model = ?", model)
		} else {
			q = q.Where("request_logs.routed_model = ?", model)
		}
	}
	q.Count(&total)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	err := q.Order("request_logs.id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error
	if err != nil {
		return logs, total, err
	}
	// 回填 provider_name：优先使用 served_provider（准确，请求时记录），
	// 对旧日志（served_provider 为空）回退到 models+providers 批量查询。
	if len(logs) > 0 {
		// Phase 1: 直接使用 served_provider
		var needLookup []string
		for i := range logs {
			if logs[i].ServedProvider != "" {
				logs[i].ProviderName = logs[i].ServedProvider
			} else {
				n := logs[i].ServedModel
				if n == "" {
					n = logs[i].RoutedModel
				}
				if n != "" {
					needLookup = append(needLookup, n)
				}
			}
		}
		// Phase 2: 对没有 served_provider 的旧日志，按模型名批量查服务商
		if len(needLookup) > 0 {
			type row struct {
				Name         string `gorm:"column:name"`
				ProviderName string `gorm:"column:provider_name"`
			}
			var rows []row
			s.DB.Table("models").
				Select("models.name, providers.name as provider_name").
				Joins("LEFT JOIN providers ON models.provider_id = providers.id").
				Where("models.name IN ?", needLookup).
				Find(&rows)
			m := make(map[string]string, len(rows))
			for _, r := range rows {
				if _, ok := m[r.Name]; !ok {
					m[r.Name] = r.ProviderName
				}
			}
			for i := range logs {
				if logs[i].ProviderName != "" {
					continue // already set from served_provider
				}
				n := logs[i].ServedModel
				if n == "" {
					n = logs[i].RoutedModel
				}
				if n != "" {
					logs[i].ProviderName = m[n]
				}
			}
		}
	}
	return logs, total, nil
}

// TokenStatRow is one row of token aggregation.
type TokenStatRow struct {
	Model            string `json:"model"`
	Provider         string `json:"provider"`
	Count            int64  `json:"count"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	CacheTokens      int64  `json:"cache_tokens"`
}

// TokenStatsTotal returns the sum of total_tokens across all logs.
func (s *Store) TokenStatsTotal() (int64, error) {
	var total int64
	err := s.DB.Model(&RequestLog{}).Where("total_tokens > 0").Where("COALESCE(route_reason, '') != 'test'").Select("COALESCE(SUM(total_tokens), 0)").Scan(&total).Error
	return total, err
}

// TokenStatsByModel aggregates token usage grouped by model + provider.
// Grouping by both avoids ambiguity when the same model name exists under
// multiple providers (e.g. "GLM-5.2" under both 智谱 and opencode).
// served_provider is set at request time (accurate); for old logs without it,
// falls back to a correlated subquery resolving provider by model name.
// Test-model logs (route_reason='test') are excluded.
func (s *Store) TokenStatsByModel() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	err := s.DB.Table("(SELECT COALESCE(NULLIF(served_model, ''), routed_model) as model, COALESCE(NULLIF(served_provider, ''), (SELECT providers.name FROM models JOIN providers ON models.provider_id = providers.id WHERE models.name = COALESCE(NULLIF(served_model, ''), routed_model) LIMIT 1)) as provider, prompt_tokens, completion_tokens, total_tokens, cache_tokens FROM request_logs WHERE COALESCE(NULLIF(served_model, ''), routed_model) != '' AND total_tokens > 0 AND COALESCE(route_reason, '') != 'test') as t").
		Select(`model,
			provider,
			count(*) as count,
			sum(prompt_tokens) as prompt_tokens,
			sum(completion_tokens) as completion_tokens,
			sum(total_tokens) as total_tokens,
			sum(cache_tokens) as cache_tokens`).
		Where("provider != ''").
		Group("model, provider").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}

// TokenStatsByProvider aggregates token usage grouped by provider name.
// Uses served_provider (accurate, set at request time) when available;
// for old logs without served_provider, falls back to resolving via
// models+providers JOIN subquery. No JOIN in the outer query avoids
// double-counting when the same model name exists under multiple providers.
// Test-model logs (route_reason='test') are excluded.
func (s *Store) TokenStatsByProvider() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	err := s.DB.Table("(SELECT COALESCE(NULLIF(served_provider, ''), (SELECT providers.name FROM models JOIN providers ON models.provider_id = providers.id WHERE models.name = COALESCE(NULLIF(served_model, ''), routed_model) LIMIT 1)) as provider, prompt_tokens, completion_tokens, total_tokens, cache_tokens FROM request_logs WHERE COALESCE(NULLIF(served_model, ''), routed_model) != '' AND total_tokens > 0 AND COALESCE(route_reason, '') != 'test') as t").
		Select(`provider,
			count(*) as count,
			sum(prompt_tokens) as prompt_tokens,
			sum(completion_tokens) as completion_tokens,
			sum(total_tokens) as total_tokens,
			sum(cache_tokens) as cache_tokens`).
		Where("provider != ''").
		Group("provider").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}

// DailyUsageRow is one day's aggregated usage.
type DailyUsageRow struct {
	Date             string `json:"date"`
	RequestCount     int64  `json:"request_count"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	CacheTokens      int64  `json:"cache_tokens"`
}

// dailyDateExpr returns a SQL expression that formats created_at as a plain
// YYYY-MM-DD string. MySQL's DATE() returns a DATE type that the driver scans
// as time.Time (RFC3339 when read into a string), so use DATE_FORMAT there;
// SQLite's date() already returns a TEXT YYYY-MM-DD.
func (s *Store) dailyDateExpr() string {
	if s.DB.Dialector.Name() == "mysql" {
		return "DATE_FORMAT(created_at, '%Y-%m-%d')"
	}
	return "date(created_at)"
}

// DailyUsageStats returns daily aggregated token usage for the given number of
// days, optionally filtered by provider (served_provider) and model
// (served_model, falling back to routed_model). It materializes a subquery that
// resolves the effective model/provider per row and applies the filters, then
// aggregates (COUNT + SUM) over it in the outer query — this avoids MySQL
// only_full_group_by issues caused by grouping on DATE()/COALESCE() expressions.
func (s *Store) DailyUsageStats(provider, model string, days int) ([]DailyUsageRow, error) {
	startDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	inner := s.DB.Table("request_logs").
		Select(s.dailyDateExpr() + " as date, COALESCE(NULLIF(served_model, ''), routed_model) as model, served_provider as provider, prompt_tokens, completion_tokens, total_tokens, cache_tokens").
		Where("created_at >= ? AND total_tokens > 0 AND COALESCE(route_reason, '') != 'test'", startDate)
	if provider != "" {
		inner = inner.Where("served_provider = ?", provider)
	}
	if model != "" {
		if s.DB.Dialector.Name() == "mysql" {
			inner = inner.Where("BINARY COALESCE(NULLIF(served_model, ''), routed_model) = ?", model)
		} else {
			inner = inner.Where("COALESCE(NULLIF(served_model, ''), routed_model) = ?", model)
		}
	}
	var rows []DailyUsageRow
	err := s.DB.Table("(?) as t", inner).
		Select("date, count(*) as request_count, sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens, sum(total_tokens) as total_tokens, sum(cache_tokens) as cache_tokens").
		Group("date").
		Order("date asc").
		Scan(&rows).Error
	return rows, err
}

// DailyUsageByModelRow is one day's aggregated usage for a single model.
type DailyUsageByModelRow struct {
	Date             string `json:"date"`
	Model            string `json:"model"`
	Provider         string `json:"provider"`
	RequestCount     int64  `json:"request_count"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	CacheTokens      int64  `json:"cache_tokens"`
}

// DailyUsageStatsByModel aggregates daily usage grouped by (date, model,
// provider), used by the stacked column chart in the usage trend page. Provider
// is included so the chart tooltip can show which provider served each model
// segment (the same model name may exist under multiple providers).
func (s *Store) DailyUsageStatsByModel(provider, model string, days int) ([]DailyUsageByModelRow, error) {
	startDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	inner := s.DB.Table("request_logs").
		Select(s.dailyDateExpr() + " as date, COALESCE(NULLIF(served_model, ''), routed_model) as model, served_provider as provider, prompt_tokens, completion_tokens, total_tokens, cache_tokens").
		Where("created_at >= ? AND total_tokens > 0 AND COALESCE(route_reason, '') != 'test'", startDate)
	if provider != "" {
		inner = inner.Where("served_provider = ?", provider)
	}
	if model != "" {
		if s.DB.Dialector.Name() == "mysql" {
			inner = inner.Where("BINARY COALESCE(NULLIF(served_model, ''), routed_model) = ?", model)
		} else {
			inner = inner.Where("COALESCE(NULLIF(served_model, ''), routed_model) = ?", model)
		}
	}
	var rows []DailyUsageByModelRow
	err := s.DB.Table("(?) as t", inner).
		Select("date, model, provider, count(*) as request_count, sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens, sum(total_tokens) as total_tokens, sum(cache_tokens) as cache_tokens").
		Group("date, model, provider").
		Order("date asc, total_tokens desc").
		Scan(&rows).Error
	return rows, err
}

// DailyUsageByQueueRow is one day's aggregated usage for a single model queue
// (routed_model, i.e. the queue name the client requested).
type DailyUsageByQueueRow struct {
	Date             string `json:"date"`
	Queue            string `json:"queue"`
	RequestCount     int64  `json:"request_count"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	CacheTokens      int64  `json:"cache_tokens"`
}

// DailyUsageStatsByQueue aggregates daily usage grouped by (date, routed_model),
// used by the stacked column chart when grouping by model queue. A queue is the
// routing target (the name the client requested); multiple served models may
// fall under the same queue, so this shows usage per queue rather than per model.
func (s *Store) DailyUsageStatsByQueue(provider, model string, days int) ([]DailyUsageByQueueRow, error) {
	startDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	inner := s.DB.Table("request_logs").
		Select(s.dailyDateExpr()+" as date, COALESCE(NULLIF(routed_model, ''), served_model) as queue, prompt_tokens, completion_tokens, total_tokens, cache_tokens").
		Where("created_at >= ? AND total_tokens > 0 AND COALESCE(route_reason, '') != 'test'", startDate)
	if provider != "" {
		inner = inner.Where("served_provider = ?", provider)
	}
	if model != "" {
		if s.DB.Dialector.Name() == "mysql" {
			inner = inner.Where("BINARY COALESCE(NULLIF(served_model, ''), routed_model) = ?", model)
		} else {
			inner = inner.Where("COALESCE(NULLIF(served_model, ''), routed_model) = ?", model)
		}
	}
	var rows []DailyUsageByQueueRow
	err := s.DB.Table("(?) as t", inner).
		Select("date, queue, count(*) as request_count, sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens, sum(total_tokens) as total_tokens, sum(cache_tokens) as cache_tokens").
		Group("date, queue").
		Order("date asc, total_tokens desc").
		Scan(&rows).Error
	return rows, err
}
