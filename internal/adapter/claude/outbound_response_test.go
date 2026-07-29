package claude

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestParseResponseText(t *testing.T) {
	raw := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet-20241022","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)

	resp, err := ParseResponse(m)
	assert.NoError(t, err)
	assert.Equal(t, "claude-3-5-sonnet-20241022", resp.Model)
	assert.Len(t, resp.Choices, 1)
	assert.Equal(t, "Hello!", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Equal(t, 10, resp.Usage.PromptTokens)
	assert.Equal(t, 5, resp.Usage.CompletionTokens)
	assert.Equal(t, 15, resp.Usage.TotalTokens)
}

func TestParseResponseToolUse(t *testing.T) {
	raw := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet-20241022","content":[{"type":"text","text":"Let me check."},{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"location":"SF"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":10}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)

	resp, err := ParseResponse(m)
	assert.NoError(t, err)
	assert.Len(t, resp.Choices, 1)
	assert.Equal(t, "Let me check.", resp.Choices[0].Message.Content)
	assert.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "tu_1", resp.Choices[0].Message.ToolCalls[0].ID)
	assert.Equal(t, "get_weather", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"location":"SF"}`, resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
	assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
}

func TestEncodeResponseToClientText(t *testing.T) {
	resp := &model.ChatResponse{
		Model: "claude-3-5-sonnet-20241022",
		Choices: []model.Choice{{
			Index:        0,
			Message:      model.Message{Role: "assistant", Content: "Hello!"},
			FinishReason: "stop",
		}},
		Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	b, err := EncodeResponseToClient(resp)
	assert.NoError(t, err)

	var m map[string]any
	_ = json.Unmarshal(b, &m)
	assert.Equal(t, "message", m["type"])
	assert.Equal(t, "assistant", m["role"])
	content := m["content"].([]any)
	assert.Len(t, content, 1)
	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "Hello!", block["text"])
	assert.Equal(t, "end_turn", m["stop_reason"])
	usage := m["usage"].(map[string]any)
	assert.Equal(t, float64(10), usage["input_tokens"])
	assert.Equal(t, float64(5), usage["output_tokens"])
}

func TestEncodeResponseToClientToolUse(t *testing.T) {
	resp := &model.ChatResponse{
		Model: "claude-3-5-sonnet-20241022",
		Choices: []model.Choice{{
			Index:   0,
			Message: model.Message{Role: "assistant", Content: "Let me check.", ToolCalls: []model.ToolCall{{
				ID:   "tu_1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "get_weather", Arguments: `{"location":"SF"}`},
			}}},
			FinishReason: "tool_calls",
		}},
	}
	b, _ := EncodeResponseToClient(resp)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	content := m["content"].([]any)
	assert.Len(t, content, 2)
	block0 := content[0].(map[string]any)
	assert.Equal(t, "text", block0["type"])
	block1 := content[1].(map[string]any)
	assert.Equal(t, "tool_use", block1["type"])
	assert.Equal(t, "tu_1", block1["id"])
	assert.Equal(t, "get_weather", block1["name"])
	assert.Equal(t, "tool_use", m["stop_reason"])
}
