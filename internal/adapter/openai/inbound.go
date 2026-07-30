package openai

import (
	"encoding/json"
	"strings"

	"auto-router/internal/model"
)

// ParseRequest converts an OpenAI chat/completions request body into canonical form.
// raw is the parsed JSON body (kept for passthrough of unknown fields).
func ParseRequest(raw map[string]any) (*model.ChatRequest, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var o struct {
		Model    string          `json:"model"`
		Messages []openaiMessage `json:"messages"`
		Tools    []model.Tool    `json:"tools"`
		Stream   bool            `json:"stream"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}
	msgs := make([]model.Message, 0, len(o.Messages))
	for _, om := range o.Messages {
		msgs = append(msgs, model.Message{
			Role:       om.Role,
			Content:    normalizeContent(om.Content),
			ToolCalls:  om.ToolCalls,
			ToolCallID: om.ToolCallID,
		})
	}
	return &model.ChatRequest{
		Model:     o.Model,
		Messages:  msgs,
		Tools:     o.Tools,
		Stream:    o.Stream,
		ClientFmt: "openai",
		Raw:       raw,
	}, nil
}

// openaiMessage is the intermediate struct for JSON unmarshaling. Content is
// kept as json.RawMessage because OpenAI allows it to be either a plain string
// or an array of content blocks (multimodal: text + image_url).
type openaiMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content"`
	ToolCalls  []model.ToolCall `json:"tool_calls"`
	ToolCallID string           `json:"tool_call_id"`
}

// normalizeContent accepts content as either a plain string or an array of
// content blocks (OpenAI multimodal format) and returns the concatenated
// text. Non-text blocks (e.g. image_url) are dropped in the canonical form;
// the original array is preserved in req.Raw for same-protocol passthrough.
func normalizeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Plain string content.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Array of content blocks: concatenate text blocks, drop others.
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
