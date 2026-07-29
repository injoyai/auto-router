package openai

import (
	"encoding/json"

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
		Messages []model.Message `json:"messages"`
		Tools    []model.Tool    `json:"tools"`
		Stream   bool            `json:"stream"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}
	return &model.ChatRequest{
		Model:     o.Model,
		Messages:  o.Messages,
		Tools:     o.Tools,
		Stream:    o.Stream,
		ClientFmt: "openai",
		Raw:       raw,
	}, nil
}
