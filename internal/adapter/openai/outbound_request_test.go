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
