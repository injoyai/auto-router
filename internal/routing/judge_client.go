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
	disp    *upstream.Dispatcher
	baseURL string
	apiKey  string
}

// Compile-time guarantee that *defaultJudgeClient satisfies JudgeClient.
var _ JudgeClient = (*defaultJudgeClient)(nil)

func NewJudgeClient(d *upstream.Dispatcher, baseURL, apiKey string) *defaultJudgeClient {
	return &defaultJudgeClient{disp: d, baseURL: baseURL, apiKey: apiKey}
}

func (j *defaultJudgeClient) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	msgs := BuildJudgeMessages(candidates, userText)
	body := map[string]any{
		"model":    judgeModel.Name,
		"messages": msgs,
		"stream":   false,
	}
	// Use a short timeout via a goroutine + select.
	done := make(chan struct {
		resp *model.ChatResponse
		err  error
	}, 1)
	go func() {
		r, err := j.disp.Call(j.baseURL, j.apiKey, body)
		done <- struct {
			resp *model.ChatResponse
			err  error
		}{r, err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			return "", res.err
		}
		if len(res.resp.Choices) == 0 {
			return "", fmt.Errorf("judge returned no choices")
		}
		return res.resp.Choices[0].Message.Content, nil
	case <-time.After(10 * time.Second):
		return "", fmt.Errorf("judge timeout")
	case <-context.Background().Done():
		return "", context.Background().Err()
	}
}
