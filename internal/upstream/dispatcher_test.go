package upstream

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestCallNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	d := New()
	resp, err := d.Call(srv.URL, "sk-test", "openai", map[string]any{"model": "gpt-4", "messages": []model.Message{{Role: "user", Content: "x"}}})
	assert.NoError(t, err)
	assert.Equal(t, "hi", resp.Choices[0].Message.Content)
}

func TestCallClaudeNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/messages", r.URL.Path)
		assert.Equal(t, "sk-claude", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	d := New()
	resp, err := d.Call(srv.URL, "sk-claude", "claude", map[string]any{"model": "claude-3", "messages": []map[string]any{{"role": "user", "content": "x"}}, "max_tokens": 100})
	assert.NoError(t, err)
	assert.Equal(t, "hi", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
}

func TestCallStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := New()
	var contents []string
	var sawDone bool
	err := d.CallStream(srv.URL, "sk-test", "openai", map[string]any{"model": "gpt-4", "stream": true}, func(ch StreamChunk) error {
		if ch == nil {
			sawDone = true
			return nil
		}
		if len(ch.Choices) > 0 {
			contents = append(contents, ch.Choices[0].Delta.Content)
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"hi"}, contents)
	assert.True(t, sawDone)
}

func TestCallClaudeStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/messages", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"role\":\"assistant\",\"model\":\"claude-3\",\"content\":[]}}\n\n")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	d := New()
	var contents []string
	var sawDone bool
	err := d.CallStream(srv.URL, "sk-claude", "claude", map[string]any{"model": "claude-3", "stream": true, "max_tokens": 100}, func(ch StreamChunk) error {
		if ch == nil {
			sawDone = true
			return nil
		}
		if len(ch.Choices) > 0 && ch.Choices[0].Delta.Content != "" {
			contents = append(contents, ch.Choices[0].Delta.Content)
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"hi"}, contents)
	assert.True(t, sawDone)
}

func TestTestModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "gpt-4o", body["model"])
		assert.Equal(t, float64(1), body["max_tokens"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"h"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	d := New()
	status, err := d.TestModel(srv.URL, "sk-test", "openai", "gpt-4o")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
}

func TestTestModelError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Invalid API key"}}`)
	}))
	defer srv.Close()

	d := New()
	status, err := d.TestModel(srv.URL, "sk-bad", "openai", "gpt-4o")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, status)
}
