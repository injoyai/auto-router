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

// startDirectiveUpstream is a request-aware mock that serves as BOTH the judge
// endpoint and the execution endpoint:
//   - judge call (request model == "judge-mini"): returns content "gpt-4o" so
//     ParseJudgeOutput picks the target model and the route reason is "judge";
//   - execution call (anything else): returns content containing a
//     <<next_model: gpt-4o>> directive so the post-processor strips it and
//     persists the session's next model.
func startDirectiveUpstream(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := "ok<<next_model: gpt-4o>>"
		if m, _ := body["model"].(string); m == "judge-mini" {
			content = "gpt-4o"
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"`+content+`"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

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
			io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"gpt-4o"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
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

func TestEndToEndDirectiveStrippedAndSessionSet(t *testing.T) {
	url := startDirectiveUpstream(t)
	app := newTestApp(t, url)

	// first request: routed by judge, execution response contains the directive
	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("X-Session-Id", "sess-x")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	// directive stripped from visible response
	assert.Equal(t, false, bytes.Contains(w.Body.Bytes(), []byte("<<next_model")))
	assert.Contains(t, w.Body.String(), "ok")

	// session now has next_model
	sess, err := app.Store.GetSession("sess-x")
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", sess.NextModel)
}

func TestEndToEndOverrideSkipsRouting(t *testing.T) {
	url := startDirectiveUpstream(t)
	app := newTestApp(t, url)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
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

// TestEndToEndDirectiveSwitchOffNotStripped verifies B2: when
// enable_next_model_directive is false, a response containing the directive is
// NOT stripped from the visible body and NOT persisted to the session.
func TestEndToEndDirectiveSwitchOffNotStripped(t *testing.T) {
	url := startDirectiveUpstream(t)
	app := newTestApp(t, url)
	// Disable the next-model directive switch.
	rc, _ := app.Store.GetRoutingConfig()
	rc.EnableNextModelDirective = false
	if err := app.Store.UpdateRoutingConfig(rc); err != nil {
		t.Fatal(err)
	}

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("X-Session-Id", "sess-off")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	// directive NOT stripped from visible response (JSON-escapes < as \u003c,
	// so we assert on the directive text which survives escaping)
	assert.Contains(t, w.Body.String(), "next_model")
	// session NOT persisted
	sess, err := app.Store.GetSession("sess-off")
	assert.Error(t, err)
	assert.Nil(t, sess)
}

// startStreamingDirectiveUpstream emits a directive split across SSE chunks so
// the streaming buffer-detect logic (I3) can be exercised:
//   - judge call (model == "judge-mini"): non-streaming JSON, content "gpt-4o";
//   - execution call (stream): "ok" then "<<next_model:" then " gpt-4o>>done".
func startStreamingDirectiveUpstream(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if m, _ := body["model"].(string); m == "judge-mini" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"gpt-4o"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: {\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"<<next_model:\"}}]}\n\n")
		io.WriteString(w, "data: {\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" gpt-4o>>done\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestEndToEndStreamingDirectiveStripped verifies I3: with real streaming and
// the directive switch on, a directive split across chunks is stripped from the
// streamed body (never leaked to the client) and persisted to the session.
func TestEndToEndStreamingDirectiveStripped(t *testing.T) {
	url := startStreamingDirectiveUpstream(t)
	app := newTestApp(t, url)
	body := `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("X-Session-Id", "sess-stream")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data: ")
	assert.Contains(t, w.Body.String(), "[DONE]")
	// directive never leaked into the streamed body
	assert.False(t, bytes.Contains(w.Body.Bytes(), []byte("next_model")))
	// visible content before and after the directive is preserved
	assert.Contains(t, w.Body.String(), "ok")
	assert.Contains(t, w.Body.String(), "done")
	// session persisted with the directive's model
	sess, err := app.Store.GetSession("sess-stream")
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", sess.NextModel)
}
