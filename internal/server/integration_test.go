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

// startStreamingUpstream is a request-aware mock:
//   - judge call (model == "judge-mini"): returns a non-streaming JSON body with
//     content "gpt-4o" (the judge client uses the non-stream path);
//   - execution call (model == "gpt-4o", stream): returns SSE chunks.
func startStreamingUpstream(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if m, _ := body["model"].(string); m == "judge-mini" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"deepseek-v4-flash"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n")
		io.WriteString(w, "data: {\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestEndToEndOverrideSkipsRouting(t *testing.T) {
	url := startMockUpstream(t)
	app := newTestApp(t, url)
	body := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	logs, _, _ := app.Store.ListLogs(1, 10, "override", "")
	assert.Len(t, logs, 1)
}

func TestEndToEndStreaming(t *testing.T) {
	url := startStreamingUpstream(t)
	app := newTestApp(t, url)
	body := `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data: ")
	assert.Contains(t, w.Body.String(), "[DONE]")
}
