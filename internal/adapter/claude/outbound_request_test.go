package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestBuildUpstreamRequestBasic(t *testing.T) {
	req := &model.ChatRequest{
		Model:   "claude-3-5-sonnet-20241022",
		Stream:  false,
		Raw:     map[string]any{"max_tokens": float64(512)},
		Messages: []model.Message{
			{Role: "system", Content: "Be helpful"},
			{Role: "user", Content: "Hi"},
		},
	}
	body, err := BuildUpstreamRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, "claude-3-5-sonnet-20241022", body["model"])
	assert.Equal(t, "Be helpful", body["system"])
	assert.Equal(t, false, body["stream"])
	assert.Equal(t, float64(512), body["max_tokens"])
	// messages should NOT include the system message (it's top-level)
	msgs := body["messages"].([]map[string]any)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0]["role"])
}

func TestBuildUpstreamRequestDefaultMaxTokens(t *testing.T) {
	req := &model.ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
		Raw:      map[string]any{},
	}
	body, _ := BuildUpstreamRequest(req)
	assert.Equal(t, 4096, body["max_tokens"])
}

func TestBuildUpstreamRequestToolCalls(t *testing.T) {
	req := &model.ChatRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []model.Message{
			{Role: "user", Content: "Weather?"},
			{Role: "assistant", Content: "Let me check.", ToolCalls: []model.ToolCall{{
				ID:   "tu_1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "get_weather", Arguments: `{"location":"SF"}`},
			}}},
			{Role: "tool", Content: "Sunny", ToolCallID: "tu_1"},
		},
		Raw: map[string]any{},
	}
	body, _ := BuildUpstreamRequest(req)
	msgs := body["messages"].([]map[string]any)
	// [user, assistant(with tool_use), user(with tool_result)]
	assert.Len(t, msgs, 3)
	// assistant message has content blocks
	assistantMsg := msgs[1]
	assert.Equal(t, "assistant", assistantMsg["role"])
	blocks := assistantMsg["content"].([]map[string]any)
	assert.True(t, len(blocks) >= 2) // text + tool_use
	// tool role message → user with tool_result
	toolMsg := msgs[2]
	assert.Equal(t, "user", toolMsg["role"])
	toolBlocks := toolMsg["content"].([]map[string]any)
	assert.Equal(t, "tool_result", toolBlocks[0]["type"])
	assert.Equal(t, "tu_1", toolBlocks[0]["tool_use_id"])
}

func TestBuildUpstreamRequestTools(t *testing.T) {
	req := &model.ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
		Tools: []model.Tool{{
			Type: "function",
			Function: struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Parameters  any    `json:"parameters"`
			}{Name: "get_weather", Description: "Get weather", Parameters: map[string]any{"type": "object"}},
		}},
		Raw: map[string]any{},
	}
	body, _ := BuildUpstreamRequest(req)
	tools := body["tools"].([]map[string]any)
	assert.Len(t, tools, 1)
	assert.Equal(t, "get_weather", tools[0]["name"])
	assert.Equal(t, "Get weather", tools[0]["description"])
	assert.NotNil(t, tools[0]["input_schema"])
}
