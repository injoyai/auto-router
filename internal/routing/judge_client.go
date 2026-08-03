package routing

import (
	"context"
	"fmt"
	"time"

	"auto-router/internal/model"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

// defaultJudgeClient is the low-level single-model judge caller used internally
// by lazyJudge. It issues requests to a judge model via the upstream dispatcher
// with retry support. Unlike routing.JudgeClient (whose Judge iterates a chain),
// defaultJudgeClient.Judge targets a single model.
type defaultJudgeClient struct {
	disp         *upstream.Dispatcher
	baseURL      string
	apiKey       string
	protocol     string
	proxyURL     string
	retryMax     int
	retryBackoff int
}

func NewJudgeClient(d *upstream.Dispatcher, baseURL, apiKey, protocol, proxyURL string, retryMax, retryBackoff int) *defaultJudgeClient {
	return &defaultJudgeClient{disp: d, baseURL: baseURL, apiKey: apiKey, protocol: protocol, proxyURL: proxyURL, retryMax: retryMax, retryBackoff: retryBackoff}
}

// Judge calls the judge model with a 10s timeout and provider-level retries.
// Returns the raw output, token usage, per-retry attempt trace, and error.
// The request body is built according to the judge provider's protocol:
// Claude requires max_tokens and a top-level system field.
func (j *defaultJudgeClient) Judge(judgeModel *store.Model, candidates []Candidate, userText string) (string, *model.Usage, []store.Attempt, error) {
	msgs := BuildJudgeMessages(candidates, userText)
	var body map[string]any
	if j.protocol == "claude" {
		// Claude format: extract system, add max_tokens
		system := ""
		var userMsgs []model.Message
		for _, m := range msgs {
			if m.Role == "system" {
				system = m.Content
			} else {
				userMsgs = append(userMsgs, m)
			}
		}
		body = map[string]any{
			"model":      judgeModel.Name,
			"max_tokens": 100,
			"system":     system,
			"messages":   userMsgs,
			"stream":     false,
		}
	} else {
		body = map[string]any{
			"model":    judgeModel.Name,
			"messages": msgs,
			"stream":   false,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var attempts []store.Attempt
	resp, _, err := j.disp.CallWithRetry(ctx, j.baseURL, j.apiKey, j.protocol, j.proxyURL, body, j.retryMax, j.retryBackoff, func(success bool, status int, e error, latencyMs int64) {
		a := store.Attempt{Type: "judge", Model: judgeModel.Name, Success: success, Status: status, LatencyMs: latencyMs}
		if e != nil {
			a.Error = e.Error()
		}
		attempts = append(attempts, a)
	})
	if err != nil {
		return "", nil, attempts, err
	}
	if len(resp.Choices) == 0 {
		return "", nil, attempts, fmt.Errorf("judge returned no choices")
	}
	usage := &resp.Usage
	return resp.Choices[0].Message.Content, usage, attempts, nil
}
