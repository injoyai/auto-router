package routing

import (
	"context"
	"fmt"
	"time"

	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

// defaultJudgeClient implements JudgeClient by calling the judge model via the
// upstream dispatcher.
type defaultJudgeClient struct {
	disp    *upstream.Dispatcher
	baseURL string
	apiKey  string
}

// Compile-time guarantee that *defaultJudgeClient satisfies JudgeClient.
var _ JudgeClient = (*defaultJudgeClient)(nil)

func NewJudgeClient(d *upstream.Dispatcher, baseURL, apiKey string) *defaultJudgeClient {
	return &defaultJudgeClient{disp: d, baseURL: baseURL, apiKey: apiKey}
}

// Judge calls the judge model with a 10s timeout. I5: the timeout is enforced
// via context.WithTimeout passed to Dispatcher.CallCtx, which builds the HTTP
// request with http.NewRequestWithContext — so the in-flight call is cancelled
// on expiry (no goroutine leak, no dead select branch).
func (j *defaultJudgeClient) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	msgs := BuildJudgeMessages(candidates, userText)
	body := map[string]any{
		"model":    judgeModel.Name,
		"messages": msgs,
		"stream":   false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := j.disp.CallCtx(ctx, j.baseURL, j.apiKey, body)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("judge returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
