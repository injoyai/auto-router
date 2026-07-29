package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// startMockClaudeUpstream returns a mock Claude upstream that asserts the
// correct path and auth headers, and returns a text response.
func startMockClaudeUpstream(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/messages", r.URL.Path)
		assert.Equal(t, "sk-claude", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3","content":[{"type":"text","text":"from claude"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestOpenAIClientToClaudeUpstream: OpenAI client sends to /v1/chat/completions,
// routed to a Claude upstream. Response is converted back to OpenAI format.
func TestOpenAIClientToClaudeUpstream(t *testing.T) {
	url := startMockClaudeUpstream(t)
	app := newTestAppWithProtocol(t, url, "claude")

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "from claude")
	assert.Contains(t, w.Body.String(), `"choices"`)
	assert.Contains(t, w.Body.String(), `"finish_reason":"stop"`)
}

// TestClaudeClientToOpenAIUpstream: Claude client sends to /v1/messages,
// routed to an OpenAI upstream. Response is converted to Claude format.
func TestClaudeClientToOpenAIUpstream(t *testing.T) {
	url := startMockUpstream(t) // OpenAI mock from integration_test.go
	app := newTestApp(t, url)

	body := `{"model":"auto","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"type":"message"`)
	assert.Contains(t, w.Body.String(), `"role":"assistant"`)
	// OpenAI mock returns "hello", converted to Claude text block
	assert.Contains(t, w.Body.String(), "hello")
	assert.Contains(t, w.Body.String(), `"stop_reason":"end_turn"`)
}

// TestClaudeClientToClaudeUpstream: Claude client → Claude upstream (same protocol).
func TestClaudeClientToClaudeUpstream(t *testing.T) {
	url := startMockClaudeUpstream(t)
	app := newTestAppWithProtocol(t, url, "claude")

	body := `{"model":"auto","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"type":"message"`)
	assert.Contains(t, w.Body.String(), "from claude")
}

// TestClaudeClientStreaming: Claude client receives Claude SSE events.
func TestClaudeClientStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	app := newTestApp(t, srv.URL)

	body := `{"model":"auto","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "message_start")
	assert.Contains(t, w.Body.String(), "content_block_delta")
	assert.Contains(t, w.Body.String(), `"text":"hi"`)
	assert.Contains(t, w.Body.String(), "message_stop")
}

// TestClaudeErrorFormat: an unauthorized request to /v1/messages is rejected
// with a 401 in the Claude error envelope (type: error), not the OpenAI shape.
func TestClaudeErrorFormat(t *testing.T) {
	app := newTestApp(t, "http://example.com")

	body := `{"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer wrong-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), `"type":"error"`)
	assert.Contains(t, w.Body.String(), `"error"`)
}
