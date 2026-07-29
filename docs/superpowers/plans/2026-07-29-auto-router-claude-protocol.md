# Auto Model Router — Claude Protocol + Cross-Protocol Routing (Plan 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Claude (Anthropic) API protocol support and cross-protocol routing so an OpenAI client can route to a Claude upstream and vice-versa, with a new `/v1/messages` endpoint for Claude clients.

**Architecture:** Internal canonical format (OpenAI-based) remains the hub. New `internal/adapter/claude/` package provides inbound (Claude→canonical), outbound request (canonical→Claude body), outbound response (Claude resp→canonical + canonical→Claude resp), and outbound stream (Claude SSE↔canonical) adapters. The dispatcher becomes protocol-aware (correct path, auth headers, response parser). The gateway gains `/v1/messages` and chooses body-building/response-encoding based on `req.ClientFmt` (client protocol) and `prov.Protocol` (upstream protocol) independently.

**Tech Stack:** Go 1.22+, Gin, GORM + glebarez/sqlite, testify/assert. No new dependencies.

**Scope of this plan:** Claude protocol adapters (inbound + outbound), dispatcher protocol-aware refactor, gateway `/v1/messages` endpoint, cross-protocol routing, integration tests. React frontend is Plan 3.

**Prerequisite:** Plan 1 completed — OpenAI protocol works end-to-end, `Provider.Protocol` field exists in the store, `model.ChatRequest.ClientFmt` field exists.

---

## File Structure

```
auto-router/
├── internal/
│   ├── adapter/
│   │   ├── openai/
│   │   │   ├── outbound_request.go      # MODIFY: whitelist passthrough for cross-protocol safety
│   │   │   └── outbound_request_test.go  # MODIFY: add cross-protocol test
│   │   └── claude/                       # NEW PACKAGE
│   │       ├── inbound.go                # Claude request → canonical
│   │       ├── inbound_test.go
│   │       ├── outbound_request.go       # canonical → Claude upstream body
│   │       ├── outbound_request_test.go
│   │       ├── outbound_response.go      # Claude resp → canonical + canonical → Claude resp
│   │       ├── outbound_response_test.go
│   │       ├── outbound_stream.go        # Claude SSE → canonical + canonical → Claude SSE
│   │       └── outbound_stream_test.go
│   ├── upstream/
│   │   ├── dispatcher.go                 # MODIFY: protocol-aware Call/CallCtx/CallStream/TestConnect
│   │   └── dispatcher_test.go            # MODIFY: add protocol param + Claude test
│   ├── routing/
│   │   └── judge_client.go               # MODIFY: protocol-aware judge body building + CallCtx
│   └── server/
│       ├── server.go                     # MODIFY: lazyJudge passes protocol + add /v1/messages route
│       ├── gateway.go                    # MODIFY: refactor to shared handleChat + protocol-aware body/resp/stream encoding
│       └── cross_protocol_test.go        # NEW: cross-protocol integration tests
```

---

## Task 1: Claude inbound adapter (Claude request → canonical)

**Files:**
- Create: `internal/adapter/claude/inbound.go`
- Test: `internal/adapter/claude/inbound_test.go`

- [ ] **Step 1: Write the failing test**

`internal/adapter/claude/inbound_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/claude/ -v`
Expected: FAIL — package doesn't exist, `ParseRequest` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/claude/inbound.go`:
```go
package claude

import (
	"encoding/json"
	"strings"

	"auto-router/internal/model"
)

// ParseRequest converts a Claude /v1/messages request body into canonical form.
// Claude-specific fields handled:
//   - top-level "system" (string or content blocks) → first system message
//   - content blocks: text → Content, tool_use → ToolCalls, tool_result → tool role message
//   - tools: {name, description, input_schema} → canonical {type:"function", function:{name, description, parameters}}
func ParseRequest(raw map[string]any) (*model.ChatRequest, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var o struct {
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		System    json.RawMessage `json:"system"`
		Messages  []claudeMessage `json:"messages"`
		Tools     []claudeTool    `json:"tools"`
		Stream    bool            `json:"stream"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}

	var msgs []model.Message

	// System field → first system message
	if sysText := parseSystemField(o.System); sysText != "" {
		msgs = append(msgs, model.Message{Role: "system", Content: sysText})
	}

	// Convert each Claude message to canonical message(s)
	for _, cm := range o.Messages {
		msgs = append(msgs, claudeMessageToCanonical(cm)...)
	}

	// Convert tools
	var tools []model.Tool
	for _, ct := range o.Tools {
		tools = append(tools, model.Tool{
			Type: "function",
			Function: struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Parameters  any    `json:"parameters"`
			}{
				Name:        ct.Name,
				Description: ct.Description,
				Parameters:  ct.InputSchema,
			},
		})
	}

	return &model.ChatRequest{
		Model:     o.Model,
		Messages:  msgs,
		Tools:     tools,
		Stream:    o.Stream,
		ClientFmt: "claude",
		Raw:       raw,
	}, nil
}

// claudeMessage is the intermediate struct for JSON unmarshaling.
type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// claudeTool is the Claude tool format.
type claudeTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

// parseSystemField handles system as string or as array of text blocks.
func parseSystemField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Try array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// claudeMessageToCanonical converts one Claude message to 1+ canonical messages.
// A single Claude message with both text and tool_result blocks expands to
// multiple canonical messages (tool_result → tool role, text → original role).
func claudeMessageToCanonical(cm claudeMessage) []model.Message {
	// Content can be a plain string
	var s string
	if json.Unmarshal(cm.Content, &s) == nil {
		return []model.Message{{Role: cm.Role, Content: s}}
	}

	// Content is an array of blocks
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}
	if json.Unmarshal(cm.Content, &blocks) != nil {
		return []model.Message{{Role: cm.Role}}
	}

	var msg model.Message
	msg.Role = cm.Role
	var extra []model.Message

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if msg.Content != "" {
				msg.Content += "\n"
			}
			msg.Content += b.Text
		case "tool_use":
			tc := model.ToolCall{
				ID:   b.ID,
				Type: "function",
			}
			tc.Function.Name = b.Name
			// Input is a JSON object; store as string
			if len(b.Input) > 0 {
				tc.Function.Arguments = string(b.Input)
			} else {
				tc.Function.Arguments = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, tc)
		case "tool_result":
			// tool_result becomes a separate "tool" role message
			resultContent := extractToolResultContent(b.Content)
			extra = append(extra, model.Message{
				Role:       "tool",
				Content:    resultContent,
				ToolCallID: b.ToolUseID,
			})
		}
	}

	result := []model.Message{}
	// If there are tool_result blocks, emit them first (they come before text in the
	// canonical ordering: tool results, then the user's new text)
	result = append(result, extra...)
	// Emit the main message only if it has content or tool_calls
	if msg.Content != "" || len(msg.ToolCalls) > 0 || len(extra) == 0 {
		result = append(result, msg)
	}
	return result
}

// extractToolResultContent handles tool_result content (string or blocks).
func extractToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/claude/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/claude/
git commit -m "feat(adapter/claude): inbound parser (Claude → canonical)"
```

---

## Task 2: Claude outbound request adapter (canonical → Claude upstream body)

Also fix `openai.BuildUpstreamRequest` to use a whitelist of safe passthrough params instead of full raw passthrough, so cross-protocol routing (Claude client → OpenAI upstream) doesn't leak Claude-specific fields into the OpenAI request.

**Files:**
- Create: `internal/adapter/claude/outbound_request.go`
- Test: `internal/adapter/claude/outbound_request_test.go`
- Modify: `internal/adapter/openai/outbound_request.go`
- Modify: `internal/adapter/openai/outbound_request_test.go`

- [ ] **Step 1: Write the failing test for Claude outbound request**

`internal/adapter/claude/outbound_request_test.go`:
```go
package claude

import (
	"encoding/json"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/claude/ -run TestBuildUpstreamRequest -v`
Expected: FAIL — `BuildUpstreamRequest` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/claude/outbound_request.go`:
```go
package claude

import (
	"encoding/json"

	"auto-router/internal/model"
)

// BuildUpstreamRequest converts a canonical request into a Claude /v1/messages
// request body map. Key conversions:
//   - First system message → top-level "system" field
//   - tool_calls → tool_use content blocks
//   - tool role messages → user messages with tool_result blocks
//   - max_tokens defaults to 4096 if not in Raw (Claude requires it)
func BuildUpstreamRequest(req *model.ChatRequest) (map[string]any, error) {
	body := map[string]any{
		"model":  req.Model,
		"stream": req.Stream,
	}

	// Extract max_tokens from Raw or default
	if mt, ok := req.Raw["max_tokens"]; ok && mt != nil {
		body["max_tokens"] = mt
	} else if mt, ok := req.Raw["max_completion_tokens"]; ok && mt != nil {
		body["max_tokens"] = mt
	} else {
		body["max_tokens"] = 4096
	}

	// Convert messages: system → top-level, others → messages array
	var msgs []map[string]any
	for _, m := range req.Messages {
		if m.Role == "system" {
			if _, exists := body["system"]; !exists {
				body["system"] = m.Content
				continue
			}
		}
		msgs = append(msgs, canonicalMessageToClaude(m))
	}
	if msgs == nil {
		msgs = []map[string]any{}
	}
	body["messages"] = msgs

	// Convert tools
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": t.Function.Parameters,
			})
		}
		body["tools"] = tools
	}

	return body, nil
}

// canonicalMessageToClaude converts one canonical message to a Claude message map.
// Tool role messages become user messages with tool_result blocks.
func canonicalMessageToClaude(m model.Message) map[string]any {
	// Tool role → user with tool_result block
	if m.Role == "tool" {
		return map[string]any{
			"role": "user",
			"content": []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}},
		}
	}

	// Assistant with tool_calls → content blocks [text?, tool_use...]
	if m.Role == "assistant" && len(m.ToolCalls) > 0 {
		var blocks []map[string]any
		if m.Content != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
		}
		for _, tc := range m.ToolCalls {
			var input any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": input,
			})
		}
		return map[string]any{"role": m.Role, "content": blocks}
	}

	// Default: simple string content
	return map[string]any{"role": m.Role, "content": m.Content}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/claude/ -v`
Expected: PASS.

- [ ] **Step 5: Fix OpenAI outbound request for cross-protocol safety**

The current `openai.BuildUpstreamRequest` does full raw passthrough which leaks Claude-specific fields when the client is Claude and upstream is OpenAI. Replace with a whitelist of safe params.

`internal/adapter/openai/outbound_request.go`:
```go
package openai

import (
	"auto-router/internal/model"
)

// passthroughKeys is the whitelist of request fields safe to copy from the
// client's raw body to an OpenAI upstream. This prevents leaking
// protocol-specific fields when cross-protocol routing (e.g. a Claude client's
// "system" top-level field or content-block "messages" must not be passed
// through to an OpenAI upstream).
var passthroughKeys = []string{
	"temperature", "top_p", "max_tokens", "max_completion_tokens",
	"stop", "presence_penalty", "frequency_penalty", "seed",
	"n", "logprobs", "top_logprobs", "response_format",
	"user",
}

// BuildUpstreamRequest converts a canonical request into an OpenAI-format
// request body map. It builds from canonical fields (model, messages, tools,
// stream) and copies a whitelist of safe params from the client's raw body.
func BuildUpstreamRequest(req *model.ChatRequest) (map[string]any, error) {
	body := map[string]any{}
	for _, k := range passthroughKeys {
		if v, ok := req.Raw[k]; ok && v != nil {
			body[k] = v
		}
	}
	body["model"] = req.Model
	body["messages"] = req.Messages
	body["stream"] = req.Stream
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	return body, nil
}
```

Add a cross-protocol test to `internal/adapter/openai/outbound_request_test.go`:
```go
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
```

- [ ] **Step 6: Run all OpenAI adapter tests to verify they pass**

Run: `go test ./internal/adapter/openai/ -v`
Expected: PASS (existing test checks temperature passthrough, still in whitelist).

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/claude/outbound_request.go internal/adapter/claude/outbound_request_test.go internal/adapter/openai/outbound_request.go internal/adapter/openai/outbound_request_test.go
git commit -m "feat(adapter/claude): outbound request builder + fix openai passthrough whitelist"
```

---

## Task 3: Claude outbound response adapter (Claude resp → canonical + canonical → Claude resp)

**Files:**
- Create: `internal/adapter/claude/outbound_response.go`
- Test: `internal/adapter/claude/outbound_response_test.go`

- [ ] **Step 1: Write the failing test**

`internal/adapter/claude/outbound_response_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/claude/ -run TestParseResponse -v`
Expected: FAIL — `ParseResponse`/`EncodeResponseToClient` undefined.

Note: the test file references `model.ChatResponse` etc. — add the import `"auto-router/internal/model"`.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/claude/outbound_response.go`:
```go
package claude

import (
	"encoding/json"

	"auto-router/internal/model"
)

// stopReasonMap converts Claude stop_reason to OpenAI finish_reason.
var stopReasonMap = map[string]string{
	"end_turn":      "stop",
	"tool_use":      "tool_calls",
	"max_tokens":    "length",
	"stop_sequence": "stop",
}

// finishReasonToClaude converts OpenAI finish_reason back to Claude stop_reason.
var finishReasonToClaude = map[string]string{
	"stop":          "end_turn",
	"tool_calls":    "tool_use",
	"length":        "max_tokens",
}

// ParseResponse converts a Claude /v1/messages non-streaming response into
// canonical form. Claude content blocks are merged: text → Content,
// tool_use → ToolCalls.
func ParseResponse(raw map[string]any) (*model.ChatResponse, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var cr struct {
		Model      string            `json:"model"`
		Content    []claudeBlock     `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      claudeUsage       `json:"usage"`
	}
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, err
	}

	msg := model.Message{Role: "assistant"}
	for _, block := range cr.Content {
		switch block.Type {
		case "text":
			if msg.Content != "" {
				msg.Content += "\n"
			}
			msg.Content += block.Text
		case "tool_use":
			args := "{}"
			if len(block.Input) > 0 {
				args = string(block.Input)
			}
			tc := model.ToolCall{
				ID:   block.ID,
				Type: "function",
			}
			tc.Function.Name = block.Name
			tc.Function.Arguments = args
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
	}

	finishReason := stopReasonMap[cr.StopReason]
	if finishReason == "" {
		finishReason = "stop"
	}

	return &model.ChatResponse{
		Model: cr.Model,
		Choices: []model.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: model.Usage{
			PromptTokens:     cr.Usage.InputTokens,
			CompletionTokens: cr.Usage.OutputTokens,
			TotalTokens:      cr.Usage.InputTokens + cr.Usage.OutputTokens,
		},
	}, nil
}

type claudeBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// EncodeResponseToClient converts a canonical response into a Claude /v1/messages
// response body. Content + tool_calls are split back into content blocks.
func EncodeResponseToClient(resp *model.ChatResponse) ([]byte, error) {
	var blocks []map[string]any
	var stopReason string

	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]
		msg := ch.Message

		if msg.Content != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": msg.Content})
		}
		for _, tc := range msg.ToolCalls {
			var input any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": input,
			})
		}
		if blocks == nil {
			blocks = []map[string]any{}
		}

		sr := finishReasonToClaude[ch.FinishReason]
		if sr == "" {
			sr = "end_turn"
		}
		stopReason = sr
	} else {
		blocks = []map[string]any{}
		stopReason = "end_turn"
	}

	out := map[string]any{
		"id":            "msg_router",
		"type":          "message",
		"role":          "assistant",
		"model":         resp.Model,
		"content":       blocks,
		"stop_reason":   stopReason,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
		},
	}
	return json.Marshal(out)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/claude/ -v`
Expected: PASS.

Note: Add the missing import `"auto-router/internal/model"` to the test file.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/claude/outbound_response.go internal/adapter/claude/outbound_response_test.go
git commit -m "feat(adapter/claude): response parser + encoder"
```

---

## Task 4: Claude outbound stream adapter (Claude SSE ↔ canonical)

**Files:**
- Create: `internal/adapter/claude/outbound_stream.go`
- Test: `internal/adapter/claude/outbound_stream_test.go`

This task implements:
1. `ParseSSELine` — parses one `data:` line of a Claude SSE stream into a canonical chunk (stateless, text-only; tool-use streaming is a known limitation).
2. `StreamEncoder` — a stateful encoder that converts canonical chunks into Claude SSE events (`message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`).

- [ ] **Step 1: Write the failing test**

`internal/adapter/claude/outbound_stream_test.go`:
```go
package claude

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestParseSSELine(t *testing.T) {
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\",\"model\":\"claude-3\",\"content\":[]}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	scanner := bufio.NewScanner(strings.NewReader(sse))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	var contents []string
	var finishReason string
	for scanner.Scan() {
		line := scanner.Text()
		ch, done, err := ParseSSELine(line)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if ch == nil {
			continue
		}
		if len(ch.Choices) > 0 {
			if ch.Choices[0].Delta.Content != "" {
				contents = append(contents, ch.Choices[0].Delta.Content)
			}
			if ch.Choices[0].FinishReason != "" {
				finishReason = ch.Choices[0].FinishReason
			}
		}
	}
	assert.Equal(t, []string{"Hel", "lo"}, contents)
	assert.Equal(t, "stop", finishReason)
}

func TestStreamEncoderText(t *testing.T) {
	enc := NewStreamEncoder("claude-3")

	// First chunk with content
	b1 := enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "Hello"}}},
	})
	s1 := string(b1)
	assert.Contains(t, s1, "message_start")
	assert.Contains(t, s1, "content_block_start")
	assert.Contains(t, s1, "content_block_delta")
	assert.Contains(t, s1, `"text":"Hello"`)

	// Second chunk
	b2 := enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "!"}}},
	})
	s2 := string(b2)
	assert.Contains(t, s2, "content_block_delta")
	assert.Contains(t, s2, `"text":"!"`)
	assert.NotContains(t, s2, "message_start") // only once

	// Finish
	b3 := enc.Finish()
	s3 := string(b3)
	assert.Contains(t, s3, "content_block_stop")
	assert.Contains(t, s3, "message_delta")
	assert.Contains(t, s3, "end_turn")
	assert.Contains(t, s3, "message_stop")
}

func TestStreamEncoderFinishReason(t *testing.T) {
	enc := NewStreamEncoder("claude-3")
	enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "Hi"}}},
	})
	b := enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{}, FinishReason: "tool_calls"}},
	})
	// FinishReason chunk should emit message_delta with tool_use
	s := string(b)
	assert.Contains(t, s, "message_delta")
	assert.Contains(t, s, "tool_use")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/claude/ -run TestParseSSELine -v`
Expected: FAIL — `ParseSSELine`, `NewStreamEncoder` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/claude/outbound_stream.go`:
```go
package claude

import (
	"encoding/json"
	"strings"

	"auto-router/internal/model"
)

// ParseSSELine parses one line of a Claude SSE stream.
// Returns (chunk, done, error). chunk is nil for non-data lines and for
// event types that don't map to canonical deltas (message_start,
// content_block_start, content_block_stop).
//
// Text streaming is fully supported. Tool-use streaming (input_json_delta)
// is not converted to canonical tool call deltas — tool calls work in
// non-streaming mode. This is a known limitation.
func ParseSSELine(line string) (*model.Chunk, bool, error) {
	if line == "" || strings.HasPrefix(line, ":") {
		return nil, false, nil
	}
	if strings.HasPrefix(line, "event:") {
		return nil, false, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil, false, nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		return nil, true, nil
	}

	var ev struct {
		Type  string          `json:"type"`
		Delta json.RawMessage `json:"delta"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil, false, err
	}

	switch ev.Type {
	case "content_block_delta":
		var d struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(ev.Delta, &d); err != nil {
			return nil, false, err
		}
		if d.Type == "text_delta" {
			return &model.Chunk{
				Choices: []model.ChunkChoice{{
					Index: 0,
					Delta: model.Delta{Content: d.Text},
				}},
			}, false, nil
		}
		return nil, false, nil

	case "message_delta":
		var d struct {
			StopReason string `json:"stop_reason"`
		}
		_ = json.Unmarshal(ev.Delta, &d)
		finish := stopReasonMap[d.StopReason]
		if finish == "" {
			finish = "stop"
		}
		return &model.Chunk{
			Choices: []model.ChunkChoice{{
				Index:        0,
				FinishReason: finish,
			}},
		}, false, nil

	case "message_stop":
		return nil, true, nil

	default:
		return nil, false, nil
	}
}

// StreamEncoder is a stateful encoder that converts canonical chunks into
// Claude SSE events. It tracks whether message_start and content_block_start
// have been emitted so they are only sent once.
type StreamEncoder struct {
	model          string
	messageStarted bool
	blockStarted   bool
	finishEmitted  bool
}

// NewStreamEncoder creates a new encoder for the given model name.
func NewStreamEncoder(modelName string) *StreamEncoder {
	return &StreamEncoder{model: modelName}
}

// EncodeChunk converts a canonical chunk to Claude SSE events (as bytes).
// Returns the full SSE text including event/data lines and \n\n separators.
// Returns nil if the chunk produces no output.
func (e *StreamEncoder) EncodeChunk(ch *model.Chunk) []byte {
	if ch == nil || len(ch.Choices) == 0 {
		return nil
	}
	choice := ch.Choices[0]
	var buf strings.Builder

	// If finish_reason is set, emit message_delta (but not message_stop yet —
	// that's in Finish())
	if choice.FinishReason != "" {
		sr := finishReasonToClaude[choice.FinishReason]
		if sr == "" {
			sr = "end_turn"
		}
		if !e.finishEmitted {
			e.finishEmitted = true
			e.ensureBlockClosed(&buf)
			ev := map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": sr},
			}
			writeSSEEvent(&buf, "message_delta", ev)
		}
		return []byte(buf.String())
	}

	if choice.Delta.Content == "" {
		return nil
	}

	// Ensure message_start + content_block_start are emitted
	if !e.messageStarted {
		e.messageStarted = true
		msgStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":         "msg_router",
				"type":       "message",
				"role":       "assistant",
				"model":      e.model,
				"content":    []any{},
				"stop_reason": nil,
				"usage":      map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		}
		writeSSEEvent(&buf, "message_start", msgStart)
	}
	if !e.blockStarted {
		e.blockStarted = true
		blockStart := map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "text", "text": ""},
		}
		writeSSEEvent(&buf, "content_block_start", blockStart)
	}

	// Emit content_block_delta
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "text_delta", "text": choice.Delta.Content},
	}
	writeSSEEvent(&buf, "content_block_delta", delta)

	return []byte(buf.String())
}

// Finish emits terminal events: content_block_stop, message_delta (if not
// already emitted), and message_stop.
func (e *StreamEncoder) Finish() []byte {
	var buf strings.Builder
	e.ensureBlockClosed(&buf)
	if !e.finishEmitted {
		e.finishEmitted = true
		writeSSEEvent(&buf, "message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
		})
	}
	writeSSEEvent(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return []byte(buf.String())
}

func (e *StreamEncoder) ensureBlockClosed(buf *strings.Builder) {
	if e.blockStarted {
		e.blockStarted = false
		writeSSEEvent(buf, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
	}
}

// writeSSEEvent writes an event: + data: pair to the buffer.
func writeSSEEvent(buf *strings.Builder, eventType string, data map[string]any) {
	b, _ := json.Marshal(data)
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(b)
	buf.WriteString("\n\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/claude/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/claude/outbound_stream.go internal/adapter/claude/outbound_stream_test.go
git commit -m "feat(adapter/claude): SSE stream parser + encoder"
```

---

## Task 5: Dispatcher protocol-aware refactor

Make the dispatcher protocol-aware: correct endpoint path (`/chat/completions` for OpenAI, `/messages` for Claude), correct auth headers (`Authorization: Bearer` for OpenAI, `x-api-key` + `anthropic-version` for Claude), and correct response/stream parsers. Update all callers (judge client, gateway) to pass the protocol.

**Files:**
- Modify: `internal/upstream/dispatcher.go`
- Modify: `internal/upstream/dispatcher_test.go`
- Modify: `internal/routing/judge_client.go`
- Modify: `internal/server/server.go` (lazyJudge passes protocol)
- Modify: `internal/server/gateway.go` (pass protocol to dispatcher)
- Modify: `internal/server/admin.go` (handleTestProvider passes protocol)

- [ ] **Step 1: Update dispatcher tests**

`internal/upstream/dispatcher_test.go` — add `protocol` param to existing tests and add a Claude test:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream/ -v`
Expected: FAIL — `Call` and `CallStream` signatures don't match (missing `protocol` param).

- [ ] **Step 3: Write minimal implementation**

Replace `internal/upstream/dispatcher.go` with:
```go
package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"auto-router/internal/adapter/claude"
	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
)

type Dispatcher struct {
	Client *http.Client
}

func New() *Dispatcher {
	return &Dispatcher{Client: &http.Client{Timeout: 5 * time.Minute}}
}

// Call performs a non-streaming upstream request and returns the parsed canonical response.
func (d *Dispatcher) Call(baseURL, apiKey, protocol string, body map[string]any) (*model.ChatResponse, error) {
	return d.CallCtx(context.Background(), baseURL, apiKey, protocol, body)
}

// CallCtx is the context-aware variant of Call.
func (d *Dispatcher) CallCtx(ctx context.Context, baseURL, apiKey, protocol string, body map[string]any) (*model.ChatResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	path := upstreamPath(protocol)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		log.Printf("[WARN] upstream %s returned %d: %s", req.URL.String(), resp.StatusCode, string(raw))
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return parseUpstreamResponse(m, protocol)
}

// StreamChunk is one parsed chunk; nil sentinel marks [DONE].
type StreamChunk = *model.Chunk

// CallStream performs a streaming upstream request and invokes onChunk for
// each parsed SSE chunk as it arrives (real streaming, not buffered).
func (d *Dispatcher) CallStream(baseURL, apiKey, protocol string, body map[string]any, onChunk func(StreamChunk) error) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	path := upstreamPath(protocol)
	req, _ := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		log.Printf("[WARN] upstream %s returned %d: %s", req.URL.String(), resp.StatusCode, string(raw))
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		ch, done, err := parseUpstreamSSELine(line, protocol)
		if err != nil {
			return err
		}
		if done {
			if onChunk != nil {
				if err := onChunk(nil); err != nil {
					return err
				}
			}
			return nil
		}
		if ch != nil && onChunk != nil {
			if err := onChunk(ch); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if onChunk != nil {
		if err := onChunk(nil); err != nil {
			return err
		}
	}
	return nil
}

// TestConnect issues a GET {baseURL}/models to verify connectivity + credentials.
func (d *Dispatcher) TestConnect(baseURL, apiKey, protocol string) (int, error) {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// upstreamPath returns the endpoint path for the given protocol.
func upstreamPath(protocol string) string {
	if protocol == "claude" {
		return "/messages"
	}
	return "/chat/completions"
}

// setUpstreamAuthHeaders sets the correct auth headers for the protocol.
func setUpstreamAuthHeaders(req *http.Request, apiKey, protocol string) {
	if protocol == "claude" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// parseUpstreamResponse dispatches to the correct protocol adapter.
func parseUpstreamResponse(m map[string]any, protocol string) (*model.ChatResponse, error) {
	if protocol == "claude" {
		return claude.ParseResponse(m)
	}
	return openai.ParseResponse(m)
}

// parseUpstreamSSELine dispatches SSE parsing to the correct protocol adapter.
func parseUpstreamSSELine(line, protocol string) (*model.Chunk, bool, error) {
	if protocol == "claude" {
		return claude.ParseSSELine(line)
	}
	return openai.ParseSSELine(line)
}
```

- [ ] **Step 4: Update judge_client.go**

`internal/routing/judge_client.go` — add `protocol` field and protocol-aware body building:
```go
package routing

import (
	"context"
	"fmt"
	"time"

	"auto-router/internal/model"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

type defaultJudgeClient struct {
	disp     *upstream.Dispatcher
	baseURL  string
	apiKey   string
	protocol string
}

var _ JudgeClient = (*defaultJudgeClient)(nil)

func NewJudgeClient(d *upstream.Dispatcher, baseURL, apiKey, protocol string) *defaultJudgeClient {
	return &defaultJudgeClient{disp: d, baseURL: baseURL, apiKey: apiKey, protocol: protocol}
}

func (j *defaultJudgeClient) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	msgs := BuildJudgeMessages(candidates, userText)
	var body map[string]any
	if j.protocol == "claude" {
		// Claude format: extract system, add max_tokens
		system := ""
		var userMsgs []model.Message
		for _, m := range msgs {
			if m.Role == "system" {
				system = m.Content
			} else {
				userMsgs = append(userMsgs, m)
			}
		}
		body = map[string]any{
			"model":      judgeModel.Name,
			"max_tokens": 100,
			"system":     system,
			"messages":   userMsgs,
			"stream":     false,
		}
	} else {
		body = map[string]any{
			"model":    judgeModel.Name,
			"messages": msgs,
			"stream":   false,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := j.disp.CallCtx(ctx, j.baseURL, j.apiKey, j.protocol, body)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("judge returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
```

- [ ] **Step 5: Update server.go (lazyJudge passes protocol)**

In `internal/server/server.go`, update `lazyJudge.Judge` to pass `prov.Protocol`:

```go
func (l *lazyJudge) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	prov, err := l.st.GetProvider(judgeModel.ProviderID)
	if err != nil {
		return "", err
	}
	apiKey, _ := store.Decrypt(l.key, prov.APIKey)
	return routing.NewJudgeClient(l.disp, prov.BaseURL, apiKey, prov.Protocol).Judge(judgeModel, candidates, userText)
}
```

- [ ] **Step 6: Update gateway.go (pass protocol to dispatcher)**

In `internal/server/gateway.go`, update the two dispatcher calls in `handleChatCompletions` and `streamResponse`:

In `handleChatCompletions`, change:
```go
resp, err := a.Dispatcher.Call(prov.BaseURL, apiKey, body)
```
to:
```go
resp, err := a.Dispatcher.Call(prov.BaseURL, apiKey, prov.Protocol, body)
```

And change the `streamResponse` call to pass `prov.Protocol`:
```go
a.streamResponse(c, prov.BaseURL, apiKey, prov.Protocol, body, dec, req, requestedModel, directiveEnabled, start)
```

Update the `streamResponse` signature to accept `protocol string` and pass it to `CallStream`:
```go
func (a *App) streamResponse(c *gin.Context, baseURL, apiKey, protocol string, body map[string]any, dec *routing.Decision, req *model.ChatRequest, requestedModel string, directiveEnabled bool, start time.Time) {
```

And inside `streamResponse`, change:
```go
streamErr := a.Dispatcher.CallStream(baseURL, apiKey, body, func(ch *model.Chunk) error {
```
to:
```go
streamErr := a.Dispatcher.CallStream(baseURL, apiKey, protocol, body, func(ch *model.Chunk) error {
```

- [ ] **Step 7: Update admin.go (handleTestProvider passes protocol)**

In `internal/server/admin.go`, change:
```go
status, err := a.Dispatcher.TestConnect(p.BaseURL, apiKey)
```
to:
```go
status, err := a.Dispatcher.TestConnect(p.BaseURL, apiKey, p.Protocol)
```

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go build ./... && go test ./... -v`
Expected: all packages PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/upstream/dispatcher.go internal/upstream/dispatcher_test.go internal/routing/judge_client.go internal/server/server.go internal/server/gateway.go internal/server/admin.go
git commit -m "feat(upstream): protocol-aware dispatcher + judge + callers"
```

---

## Task 6: Gateway /v1/messages + cross-protocol routing + integration tests

Refactor `handleChatCompletions` into a shared `handleChat` that accepts the inbound parser. Add `handleMessages` for Claude clients. Make body building, response encoding, stream encoding, and error format protocol-aware. Add cross-protocol integration tests.

**Files:**
- Modify: `internal/server/gateway.go`
- Modify: `internal/server/server.go` (add `/v1/messages` route)
- Create: `internal/server/cross_protocol_test.go`

- [ ] **Step 1: Write the failing integration tests**

`internal/server/cross_protocol_test.go`:
```go
package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Mock Claude upstream returning a text response.
func startMockClaudeUpstream(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/messages", r.URL.Path)
		assert.Equal(t, "sk-claude", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3","content":[{"type":"text","text":"from claude"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
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

// TestClaudeErrorFormat: errors returned to Claude clients use Claude error format.
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
```

- [ ] **Step 2: Add test helper for protocol-specific provider**

Add to `internal/server/apptest_test.go` (or `cross_protocol_test.go`):
```go
func newTestAppWithProtocol(t *testing.T, upstreamURL, protocol string) *testApp {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	key := store.DeriveKey("test-seed")
	prov := &store.Provider{Name: "p", BaseURL: upstreamURL, APIKey: store.Encrypt(key, "sk-claude"), Protocol: protocol, Enabled: true}
	_ = st.CreateProvider(prov)
	judge := &store.Model{Name: "judge-mini", DisplayName: "Judge", ProviderID: prov.ID, Enabled: true}
	_ = st.CreateModel(judge)
	_ = st.SetJudgeModel(judge.ID)
	target := &store.Model{Name: "claude-3", DisplayName: "Claude", ProviderID: prov.ID, Enabled: true}
	_ = st.CreateModel(target)
	_ = st.UpdateRoutingConfig(&store.RoutingConfig{ID: 1, JudgeModelID: &judge.ID, DefaultModelID: &target.ID, EnableNextModelDirective: true, SessionTTLSeconds: 1800, JudgeMaxInputChars: 2000})

	cfg := config.Config{}
	app := NewApp(cfg, st, key, "gw-token", "admin-token")
	return &testApp{App: app, UpstreamURL: upstreamURL}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestClaudeClient -v`
Expected: FAIL — `/v1/messages` route doesn't exist, `newTestAppWithProtocol` undefined.

- [ ] **Step 4: Refactor gateway.go into shared handleChat**

Replace `internal/server/gateway.go` with the refactored version. Key changes:
- Extract `handleChat` shared method that takes `clientFmt` + `parseInbound` function
- `handleChatCompletions` and `handleMessages` are thin wrappers
- Body building is protocol-aware (based on `prov.Protocol`)
- Response encoding is protocol-aware (based on `req.ClientFmt`)
- Stream encoding uses `chunkEncoder` interface (OpenAI stateless, Claude stateful)
- Error responses use `writeGatewayError` which respects client format

```go
package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/adapter/claude"
	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
	"auto-router/internal/routing"
	"auto-router/internal/store"
)

const directiveOpen = "<<next_model:"

const nextModelDirectiveInjection = "你可以在回复中用 <<next_model: 模型名>> 指定下一轮应使用的模型,该标记不会展示给用户。"

func directiveHoldback(s string) int {
	maxK := len(s)
	if maxK > len(directiveOpen) {
		maxK = len(directiveOpen)
	}
	for k := maxK; k > 0; k-- {
		if strings.HasSuffix(s, directiveOpen[:k]) {
			return k
		}
	}
	return 0
}

func injectNextModelDirective(req *model.ChatRequest) {
	for i := range req.Messages {
		if req.Messages[i].Role == "system" {
			req.Messages[i].Content += "\n" + nextModelDirectiveInjection
			return
		}
	}
	req.Messages = append([]model.Message{{Role: "system", Content: nextModelDirectiveInjection}}, req.Messages...)
}

// chunkEncoder encodes canonical chunks into the client's SSE format.
type chunkEncoder interface {
	EncodeChunk(ch *model.Chunk) []byte
	Finish() []byte
}

// openaiChunkEncoder is stateless; each chunk is one data: line + \n\n.
type openaiChunkEncoder struct{}

func (openaiChunkEncoder) EncodeChunk(ch *model.Chunk) []byte {
	b, _ := openai.EncodeChunk(ch)
	return append(b, '\n', '\n')
}
func (openaiChunkEncoder) Finish() []byte { return []byte("data: [DONE]\n\n") }

// claudeChunkEncoder wraps claude.StreamEncoder.
type claudeChunkEncoder struct{ enc *claude.StreamEncoder }

func (e *claudeChunkEncoder) EncodeChunk(ch *model.Chunk) []byte { return e.enc.EncodeChunk(ch) }
func (e *claudeChunkEncoder) Finish() []byte                      { return e.enc.Finish() }

// writeGatewayError writes an error in the client's protocol format.
func writeGatewayError(c *gin.Context, status int, clientFmt, msg, errType string) {
	if clientFmt == "claude" {
		c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": errType, "message": msg}})
	} else {
		c.JSON(status, gin.H{"error": gin.H{"message": msg, "type": errType}})
	}
}

// handleChat is the shared handler for both /v1/chat/completions and /v1/messages.
func (a *App) handleChat(c *gin.Context, clientFmt string, parseInbound func(map[string]any) (*model.ChatRequest, error)) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		writeGatewayError(c, http.StatusBadRequest, clientFmt, err.Error(), "invalid_request_error")
		return
	}
	req, err := parseInbound(raw)
	if err != nil {
		writeGatewayError(c, http.StatusBadRequest, clientFmt, err.Error(), "invalid_request_error")
		return
	}
	req.SessionID = c.GetHeader("X-Session-Id")
	if m := c.GetHeader("X-Route-Model"); m != "" {
		req.Override = m
	} else if !req.IsRouteRequested() {
		req.Override = req.Model
	}

	start := time.Now()
	dec, err := a.Engine.Route(req)
	if err != nil {
		writeGatewayError(c, http.StatusServiceUnavailable, clientFmt, err.Error(), "router_error")
		return
	}

	requestedModel := req.Model

	rc, _ := a.Store.GetRoutingConfig()
	directiveEnabled := rc != nil && rc.EnableNextModelDirective && req.SessionID != ""
	if directiveEnabled {
		injectNextModelDirective(req)
	}

	prov, err := a.Store.GetProvider(dec.Model.ProviderID)
	if err != nil {
		writeGatewayError(c, http.StatusServiceUnavailable, clientFmt, "provider not found", "router_error")
		return
	}
	apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)

	req.Model = dec.Model.Name

	// Build upstream body based on UPSTREAM protocol
	var body map[string]any
	if prov.Protocol == "claude" {
		body, _ = claude.BuildUpstreamRequest(req)
	} else {
		body, _ = openai.BuildUpstreamRequest(req)
	}

	status := http.StatusOK
	errMsg := ""
	if req.Stream {
		a.streamResponse(c, prov.BaseURL, apiKey, prov.Protocol, body, dec, req, requestedModel, directiveEnabled, start)
		return
	}
	resp, err := a.Dispatcher.Call(prov.BaseURL, apiKey, prov.Protocol, body)
	if err != nil {
		status = http.StatusBadGateway
		errMsg = err.Error()
		writeGatewayError(c, status, clientFmt, err.Error(), "upstream_error")
	} else {
		a.postProcessResponse(req, resp, directiveEnabled)
		// Encode response based on CLIENT protocol
		var b []byte
		if req.ClientFmt == "claude" {
			b, _ = claude.EncodeResponseToClient(resp)
		} else {
			b, _ = openai.EncodeResponseToClient(resp)
		}
		c.Data(http.StatusOK, "application/json", b)
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg)
}

func (a *App) handleChatCompletions(c *gin.Context) {
	a.handleChat(c, "openai", openai.ParseRequest)
}

func (a *App) handleMessages(c *gin.Context) {
	a.handleChat(c, "claude", claude.ParseRequest)
}
```

- [ ] **Step 5: Update streamResponse to use chunkEncoder**

Replace the `streamResponse` function with a version that uses the `chunkEncoder` interface for protocol-aware encoding:

```go
func (a *App) streamResponse(c *gin.Context, baseURL, apiKey, protocol string, body map[string]any, dec *routing.Decision, req *model.ChatRequest, requestedModel string, directiveEnabled bool, start time.Time) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, _ := c.Writer.(http.Flusher)
	status := http.StatusOK
	errMsg := ""

	// Choose encoder based on CLIENT protocol
	var enc chunkEncoder
	if req.ClientFmt == "claude" {
		enc = &claudeChunkEncoder{enc: claude.NewStreamEncoder(dec.ModelName)}
	} else {
		enc = openaiChunkEncoder{}
	}

	var assembled strings.Builder
	flushed := 0
	directiveSeen := false

	flushText := func(text string) {
		if text == "" {
			return
		}
		ch := &model.Chunk{Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: text}}}}
		c.Writer.Write(enc.EncodeChunk(ch))
		flusher.Flush()
	}

	streamErr := a.Dispatcher.CallStream(baseURL, apiKey, protocol, body, func(ch *model.Chunk) error {
		if ch == nil {
			full := assembled.String()
			if directiveEnabled {
				clean, mname := routing.ExtractNextModel(full)
				if mname != "" {
					a.persistNextModel(req, mname)
				}
				if rem := clean[flushed:]; rem != "" {
					flushText(rem)
				}
			} else if rem := full[flushed:]; rem != "" {
				flushText(rem)
			}
			c.Writer.Write(enc.Finish())
			flusher.Flush()
			return nil
		}
		if !directiveEnabled {
			c.Writer.Write(enc.EncodeChunk(ch))
			flusher.Flush()
			return nil
		}
		if len(ch.Choices) > 0 {
			assembled.WriteString(ch.Choices[0].Delta.Content)
		}
		if directiveSeen {
			return nil
		}
		full := assembled.String()
		if idx := strings.Index(full, directiveOpen); idx >= 0 {
			if idx > flushed {
				flushText(full[flushed:idx])
				flushed = idx
			}
			directiveSeen = true
			return nil
		}
		hold := directiveHoldback(full[flushed:])
		safeEnd := len(full) - hold
		if safeEnd > flushed {
			flushText(full[flushed:safeEnd])
			flushed = safeEnd
		}
		return nil
	})
	if streamErr != nil {
		status = http.StatusBadGateway
		errMsg = streamErr.Error()
		writeGatewayError(c, status, req.ClientFmt, streamErr.Error(), "upstream_error")
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg)
}
```

Keep `postProcessResponse`, `persistNextModel`, `writeLog`, and `handleListModels` unchanged.

- [ ] **Step 6: Add /v1/messages route in server.go**

In `internal/server/server.go`, add the route after `/chat/completions`:
```go
	v1.POST("/chat/completions", app.handleChatCompletions)
	v1.POST("/messages", app.handleMessages)
	v1.GET("/models", app.handleListModels)
```

- [ ] **Step 7: Run all tests**

Run: `go build ./... && go test ./... -v`
Expected: all PASS including new cross-protocol tests.

- [ ] **Step 8: Commit**

```bash
git add internal/server/gateway.go internal/server/server.go internal/server/cross_protocol_test.go internal/server/apptest_test.go
git commit -m "feat(server): /v1/messages endpoint + cross-protocol routing"
```

---

## Self-Review

**1. Spec coverage (Plan 2 scope):**
- Claude inbound adapter (system, content blocks, tool_use, tool_result, tools) → Task 1 ✓
- Claude outbound request (canonical→Claude body, system extraction, tool_use blocks, max_tokens default) → Task 2 ✓
- OpenAI outbound request fixed for cross-protocol (whitelist passthrough) → Task 2 ✓
- Claude outbound response (Claude resp→canonical, canonical→Claude resp, stop_reason mapping) → Task 3 ✓
- Claude outbound stream (SSE parser + stateful encoder) → Task 4 ✓
- Dispatcher protocol-aware (path, auth headers, response/stream parser) → Task 5 ✓
- Judge client protocol-aware → Task 5 ✓
- Gateway `/v1/messages` endpoint → Task 6 ✓
- Cross-protocol body building (based on upstream protocol) → Task 6 ✓
- Cross-protocol response encoding (based on client protocol) → Task 6 ✓
- Cross-protocol stream encoding (chunkEncoder interface) → Task 6 ✓
- Claude error format → Task 6 ✓
- Integration tests (OpenAI→Claude, Claude→OpenAI, Claude→Claude, streaming, error) → Task 6 ✓
- **Out of scope (Plan 3):** React frontend
- **Known limitation:** Tool-use in streaming mode (input_json_delta) is not converted to canonical tool call deltas. Tool calls work in non-streaming mode.

**2. Placeholder scan:** None. All steps contain real code. Task 6's test `TestClaudeClientToOpenAIUpstream` has a corrected assertion (OpenAI mock returns "hello", not "from claude").

**3. Type consistency:**
- `Dispatcher.Call(baseURL, apiKey, protocol, body)` — consistent in Tasks 5, 6
- `Dispatcher.CallStream(baseURL, apiKey, protocol, body, onChunk)` — consistent
- `Dispatcher.CallCtx(ctx, baseURL, apiKey, protocol, body)` — consistent
- `Dispatcher.TestConnect(baseURL, apiKey, protocol)` — consistent
- `NewJudgeClient(d, baseURL, apiKey, protocol)` — consistent between Task 5 judge_client.go and server.go lazyJudge
- `claude.NewStreamEncoder(modelName)` — consistent between Task 4 and Task 6
- `chunkEncoder` interface — `EncodeChunk`/`Finish` consistent between openaiChunkEncoder and claudeChunkEncoder

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-29-auto-router-claude-protocol.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
