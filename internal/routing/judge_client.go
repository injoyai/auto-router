package routing

import (
	"context"
	"fmt"
	"time"

	"auto-router/internal/model"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

// defaultJudgeClient implements JudgeClient by calling the judge model via the
// upstream dispatcher.
type defaultJudgeClient struct {
	disp     *upstream.Dispatcher
	baseURL  string
	apiKey   string
	protocol string
	proxyURL string
}

func NewJudgeClient(d *upstream.Dispatcher, baseURL, apiKey, protocol, proxyURL string) *defaultJudgeClient {
	return &defaultJudgeClient{disp: d, baseURL: baseURL, apiKey: apiKey, protocol: protocol, proxyURL: proxyURL}
}

// Judge calls the judge model with a 10s timeout. The request body is built
// according to the judge provider's protocol: Claude requires max_tokens and
// a top-level system field (extracted from the first system message).
func (j *defaultJudgeClient) Judge(judgeModel *store.Model, candidates []Candidate, userText string) (string, *model.Usage, error) {
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
	resp, err := j.disp.CallCtx(ctx, j.baseURL, j.apiKey, j.protocol, j.proxyURL, body)
	if err != nil {
		return "", nil, err
	}
	if len(resp.Choices) == 0 {
		return "", nil, fmt.Errorf("judge returned no choices")
	}
	usage := &resp.Usage
	return resp.Choices[0].Message.Content, usage, nil
}
