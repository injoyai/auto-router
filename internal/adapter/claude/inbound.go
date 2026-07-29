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
