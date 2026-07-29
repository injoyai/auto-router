package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestBuildUpstreamRequest(t *testing.T) {
	req := &model.ChatRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
		Raw:      map[string]any{"temperature": 0.7},
	}
	body, err := BuildUpstreamRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", body["model"])
	assert.Equal(t, true, body["stream"])
	assert.Equal(t, 0.7, body["temperature"])
	assert.NotNil(t, body["messages"])
}

func TestBuildUpstreamRequestCrossProtocolNoLeak(t *testing.T) {
	// Simulate a Claude client's raw body (has "system", content-block "messages")
	req := &model.ChatRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Raw: map[string]any{
			"system":      "should not leak",
			"max_tokens":  float64(100),
			"temperature": float64(0.5),
		},
	}
	body, err := BuildUpstreamRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", body["model"])
	assert.Equal(t, float64(0.5), body["temperature"])
	assert.Equal(t, float64(100), body["max_tokens"])
	// Claude-specific fields must NOT leak
	_, hasSystem := body["system"]
	assert.False(t, hasSystem, "system field must not leak from Claude client raw")
}

func TestBuildUpstreamRequestToolChoicePassthrough(t *testing.T) {
	req := &model.ChatRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Raw: map[string]any{
			"tool_choice":         "auto",
			"stream_options":      map[string]any{"include_usage": true},
			"parallel_tool_calls": true,
		},
	}
	body, err := BuildUpstreamRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, "auto", body["tool_choice"])
	assert.NotNil(t, body["stream_options"])
	assert.Equal(t, true, body["parallel_tool_calls"])
}
