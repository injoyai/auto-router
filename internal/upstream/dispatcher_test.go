package upstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestCallNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	d := New()
	resp, err := d.Call(srv.URL, "sk-test", map[string]any{"model": "gpt-4", "messages": []model.Message{{Role: "user", Content: "x"}}})
	assert.NoError(t, err)
	assert.Equal(t, "hi", resp.Choices[0].Message.Content)
}

func TestCallStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := New()
	chunks, err := d.CallStream(srv.URL, "sk-test", map[string]any{"model": "gpt-4", "stream": true})
	assert.NoError(t, err)
	assert.Equal(t, "hi", chunks[0].Choices[0].Delta.Content)
	assert.True(t, chunks[1] == nil) // sentinel [DONE]
}
