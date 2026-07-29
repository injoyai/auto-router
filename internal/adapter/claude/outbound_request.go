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
			if existing, exists := body["system"]; exists {
				body["system"] = existing.(string) + "\n" + m.Content
				continue
			}
			body["system"] = m.Content
			continue
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
			input := map[string]any{}
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
