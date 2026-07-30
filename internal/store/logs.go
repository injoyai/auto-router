package store

import "time"

type RequestLog struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	SessionID      string    `json:"session_id"`
	ClientProtocol string    `json:"client_protocol"`
	RequestedModel string    `json:"requested_model"`
	RoutedModel    string    `json:"routed_model"`
	RouteReason    string    `json:"route_reason"`
	JudgeRaw       string    `json:"judge_raw"`
	Status         int       `json:"status"`
	LatencyMs      int64     `json:"latency_ms"`
	Error          string    `json:"error"`
	RetryCount    int       `json:"retry_count"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Store) CreateLog(l *RequestLog) error {
	return s.DB.Create(l).Error
}

func (s *Store) ListLogs(page, pageSize int, reason, model string) ([]RequestLog, int64, error) {
	var logs []RequestLog
	var total int64
	q := s.DB.Model(&RequestLog{})
	if reason != "" {
		q = q.Where("route_reason = ?", reason)
	}
	if model != "" {
		q = q.Where("routed_model = ?", model)
	}
	q.Count(&total)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error
	return logs, total, err
}
