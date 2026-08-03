package openai

import (
	"encoding/json"

	"auto-router/internal/model"
)

// ParseResponse converts an OpenAI-format non-streaming response into canonical form.
func ParseResponse(raw map[string]any) (*model.ChatResponse, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var r model.ChatResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	// Extract cache hit tokens from OpenAI's nested prompt_tokens_details.cached_tokens
	var details struct {
		Usage struct {
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	json.Unmarshal(b, &details)
	r.Usage.CacheTokens = details.Usage.PromptTokensDetails.CachedTokens
	return &r, nil
}

// EncodeResponseToClient returns the canonical response as an OpenAI-format body.
// (OpenAI is the canonical format, so this is a passthrough marshal.)
func EncodeResponseToClient(resp *model.ChatResponse) ([]byte, error) {
	return json.Marshal(resp)
}
