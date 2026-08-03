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
	"stop":       "end_turn",
	"tool_calls": "tool_use",
	"length":     "max_tokens",
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
		Model      string        `json:"model"`
		Content    []claudeBlock `json:"content"`
		StopReason string        `json:"stop_reason"`
		Usage      claudeUsage   `json:"usage"`
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
			if len(block.Input) > 0 && string(block.Input) != "null" {
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
			CacheTokens:      cr.Usage.CacheReadInputTokens,
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
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	CacheReadInputTokens  int `json:"cache_read_input_tokens"`
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
		"id":          "msg_router",
		"type":        "message",
		"role":        "assistant",
		"model":       resp.Model,
		"content":     blocks,
		"stop_reason": stopReason,
		"usage": map[string]any{
			"input_tokens":             resp.Usage.PromptTokens,
			"output_tokens":            resp.Usage.CompletionTokens,
			"cache_read_input_tokens":  resp.Usage.CacheTokens,
		},
	}
	return json.Marshal(out)
}
