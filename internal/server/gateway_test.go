package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// startMockUpstream serves as BOTH the judge model endpoint and the target
// model endpoint. It inspects the incoming request model:
//   - judge call (model == "judge-mini"): returns content "gpt-4o" so the
//     judge parser picks the target model and the route reason is "judge";
//   - target call (anything else): returns content "hello" so the client
//     receives a recognizable body.
//
// The plan's original mock returned a single fixed body, which made the judge
// reply "hello" — ParseJudgeOutput could not match any model name, so the
// engine fell back to the default model and the log reason became "fallback"
// instead of the spec-intended "judge". This request-aware variant keeps both
// the response-content assertion and the reason="judge" assertion valid.
func startMockUpstream(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := "hello"
		if m, _ := body["model"].(string); m == "judge-mini" {
			content = "deepseek-v4-flash"
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"`+content+`"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGatewayNonStreamRoute(t *testing.T) {
	upstreamURL := startMockUpstream(t)
	app := newTestApp(t, upstreamURL)
	r := app.Router

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "hello")
	// log written with reason "judge"
	logs, total, _ := app.Store.ListLogs(1, 10, "judge", "")
	assert.Equal(t, int64(1), total)
	assert.NotEmpty(t, logs)
}

// TestGatewayRequestedVsRoutedModel verifies B1: the request log stores the
// client's original model (e.g. "auto") in RequestedModel and the chosen model
// in RoutedModel, even though req.Model is overwritten before dispatch.
func TestGatewayRequestedVsRoutedModel(t *testing.T) {
	upstreamURL := startMockUpstream(t)
	app := newTestApp(t, upstreamURL)
	r := app.Router

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// One merged log: the judge call is no longer a separate row; its
	// diagnostics live on the single reason=judge entry.
	logs, _, _ := app.Store.ListLogs(1, 10, "judge", "")
	if assert.Len(t, logs, 1) {
		assert.Equal(t, "auto", logs[0].RequestedModel)
		assert.Equal(t, "deepseek-v4-flash", logs[0].RoutedModel)
		assert.NotEqual(t, logs[0].RequestedModel, logs[0].RoutedModel)
		// Judge diagnostics are inlined on the merged row.
		assert.Equal(t, "judge-mini", logs[0].JudgeModel)
		assert.NotEmpty(t, logs[0].JudgeRaw)
		assert.Equal(t, 1, logs[0].JudgeTotalTokens)
	}
}
