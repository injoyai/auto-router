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

// UpdateLogTrace updates the trace field (and served_model) of an existing log.
// Used for real-time progress: each attempt appends to the trace so the frontend
// can observe the chain growing before the request finishes.
func (s *Store) UpdateLogTrace(id uint, trace, servedModel string) error {
	return s.DB.Model(&RequestLog{}).Where("id = ?", id).
		Updates(map[string]any{"trace": trace, "served_model": servedModel}).Error
}

// UpdateLogFinal writes the final state of a request log (status, latency,
// error, retry count, token usage, judge diagnostics, served model, failover).
func (s *Store) UpdateLogFinal(id uint, status int, latencyMs int64, errMsg string, retryCount int, servedModel string, failoverCount int, usage *model.Usage, judgeRaw, judgeModel string, judgeLatencyMs int64, judgeUsage *model.Usage) error {
	updates := map[string]any{
		"status":           status,
		"latency_ms":       latencyMs,
		"error":            errMsg,
		"retry_count":      retryCount,
		"served_model":     servedModel,
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
		q = q.Where("request_logs.routed_model = ?", model)
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
	// 批量回填 provider_name：仅查本页涉及的去重模型名，最多 pageSize 条
	if len(logs) > 0 {
		names := make(map[string]struct{})
		for _, l := range logs {
			n := l.ServedModel
			if n == "" {
				n = l.RoutedModel
			}
			if n != "" {
				names[n] = struct{}{}
			}
		}
		if len(names) > 0 {
			list := make([]string, 0, len(names))
			for n := range names {
				list = append(list, n)
			}
			type row struct {
				Name         string `gorm:"column:name"`
				ProviderName string `gorm:"column:provider_name"`
			}
			var rows []row
			s.DB.Table("models").
				Select("models.name, providers.name as provider_name").
				Joins("LEFT JOIN providers ON models.provider_id = providers.id").
				Where("models.name IN ?", list).
				Find(&rows)
			m := make(map[string]string, len(rows))
			for _, r := range rows {
				if _, ok := m[r.Name]; !ok {
					m[r.Name] = r.ProviderName
				}
			}
			for i := range logs {
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
	err := s.DB.Model(&RequestLog{}).Where("total_tokens > 0").Select("COALESCE(SUM(total_tokens), 0)").Scan(&total).Error
	return total, err
}

// TokenStatsByModel aggregates token usage grouped by the model that actually
// served the request (served_model, falling back to routed_model when
// served_model is unset/NULL), ordered by total_tokens desc, limited to 10.
// NULLIF is needed because GORM persists unset string fields as '' (not NULL),
// which would otherwise prevent COALESCE from falling back to routed_model.
// A correlated subquery resolves the provider name for each model (picks the
// first match if a model name exists under multiple providers).
func (s *Store) TokenStatsByModel() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	// 用子查询先将 model 表达式物化为列,外层 GROUP BY 该列。
	// 避免直接 GROUP BY COALESCE(...) 时,SELECT 中的子查询引用 served_model
	// 触发 MySQL only_full_group_by 报错(Error 1055)。
	err := s.DB.Table("(SELECT COALESCE(NULLIF(served_model, ''), routed_model) as model, prompt_tokens, completion_tokens, total_tokens, cache_tokens FROM request_logs WHERE COALESCE(NULLIF(served_model, ''), routed_model) != '' AND total_tokens > 0) as t").
		Select(`model,
			(SELECT providers.name FROM models JOIN providers ON models.provider_id = providers.id WHERE models.name = model LIMIT 1) as provider,
			count(*) as count,
			sum(prompt_tokens) as prompt_tokens,
			sum(completion_tokens) as completion_tokens,
			sum(total_tokens) as total_tokens,
			sum(cache_tokens) as cache_tokens`).
		Group("model").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}

// TokenStatsByProvider aggregates token usage grouped by provider name,
// joining models+providers to resolve the provider name. The join uses the
// model that actually served the request (served_model, falling back to
// routed_model). Limited to 10.
func (s *Store) TokenStatsByProvider() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	err := s.DB.Table("request_logs").
		Select("providers.name as provider, count(*) as count, sum(request_logs.prompt_tokens) as prompt_tokens, sum(request_logs.completion_tokens) as completion_tokens, sum(request_logs.total_tokens) as total_tokens, sum(request_logs.cache_tokens) as cache_tokens").
		Joins("LEFT JOIN models ON COALESCE(NULLIF(request_logs.served_model, ''), request_logs.routed_model) = models.name").
		Joins("LEFT JOIN providers ON models.provider_id = providers.id").
		Where("COALESCE(NULLIF(request_logs.served_model, ''), request_logs.routed_model) != '' AND request_logs.total_tokens > 0 AND providers.name != ''").
		Group("providers.name").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}
