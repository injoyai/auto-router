package claude

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestParseRequestSimpleString(t *testing.T) {
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":1024,"system":"You are helpful","messages":[{"role":"user","content":"Hello"}],"stream":false}`
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)

	req, err := ParseRequest(raw)
	assert.NoError(t, err)
	assert.Equal(t, "claude-3-5-sonnet-20241022", req.Model)
	assert.Equal(t, "claude", req.ClientFmt)
	assert.False(t, req.Stream)
	// system field → first system message
	assert.Len(t, req.Messages, 2)
	assert.Equal(t, "system", req.Messages[0].Role)
	assert.Equal(t, "You are helpful", req.Messages[0].Content)
	assert.Equal(t, "user", req.Messages[1].Role)
	assert.Equal(t, "Hello", req.Messages[1].Content)
}

func TestParseRequestContentBlocksMixed(t *testing.T) {
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":1024,"messages":[
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"Sunny"},{"type":"text","text":"Thanks"}]}
	]}`
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)

	req, err := ParseRequest(raw)
	assert.NoError(t, err)
	// tool_result → tool message, text → user message
	assert.GreaterOrEqual(t, len(req.Messages), 2)
	var toolMsg *model.Message
	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			toolMsg = &req.Messages[i]
			break
		}
	}
	assert.NotNil(t, toolMsg, "should have a tool role message from tool_result block")
	assert.Equal(t, "tu_1", toolMsg.ToolCallID)
	assert.Equal(t, "Sunny", toolMsg.Content)
}

func TestParseRequestAssistantToolUse(t *testing.T) {
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":1024,"messages":[
		{"role":"user","content":"What's the weather?"},
		{"role":"assistant","content":[{"type":"text","text":"Let me check."},{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"location":"SF"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"Sunny"}]}
	]}`
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)

	req, err := ParseRequest(raw)
	assert.NoError(t, err)
	// Messages: [user, assistant(with tool_calls), tool]
	assert.Len(t, req.Messages, 3)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "assistant", req.Messages[1].Role)
	assert.Contains(t, req.Messages[1].Content, "Let me check.")
	assert.Len(t, req.Messages[1].ToolCalls, 1)
	assert.Equal(t, "tu_1", req.Messages[1].ToolCalls[0].ID)
	assert.Equal(t, "get_weather", req.Messages[1].ToolCalls[0].Function.Name)
	assert.Equal(t, `{"location":"SF"}`, req.Messages[1].ToolCalls[0].Function.Arguments)
	assert.Equal(t, "tool", req.Messages[2].Role)
	assert.Equal(t, "tu_1", req.Messages[2].ToolCallID)
	assert.Equal(t, "Sunny", req.Messages[2].Content)
}

func TestParseRequestSystemAsBlocks(t *testing.T) {
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":1024,"system":[{"type":"text","text":"Be concise"}],"messages":[{"role":"user","content":"Hi"}]}`
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)

	req, err := ParseRequest(raw)
	assert.NoError(t, err)
	assert.Equal(t, "system", req.Messages[0].Role)
	assert.Equal(t, "Be concise", req.Messages[0].Content)
}

func TestParseRequestRouteSentinel(t *testing.T) {
	for _, m := range []string{"", "auto", "route"} {
		req, _ := ParseRequest(map[string]any{"model": m, "max_tokens": 100, "messages": []any{}})
		assert.True(t, req.IsRouteRequested(), "model=%q should trigger routing", m)
	}
}

func TestParseRequestTools(t *testing.T) {
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":1024,"tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"location":{"type":"string"}}}}],"messages":[{"role":"user","content":"Hi"}]}`
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)

	req, err := ParseRequest(raw)
	assert.NoError(t, err)
	assert.Len(t, req.Tools, 1)
	assert.Equal(t, "function", req.Tools[0].Type)
	assert.Equal(t, "get_weather", req.Tools[0].Function.Name)
	assert.Equal(t, "Get weather", req.Tools[0].Function.Description)
}
