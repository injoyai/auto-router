package openai

import (
	"auto-router/internal/model"
)

// BuildUpstreamRequest converts a canonical request into an OpenAI-format
// request body map. It starts from the client's raw body (passthrough) and
// forces the model + messages + stream fields.
func BuildUpstreamRequest(req *model.ChatRequest) (map[string]any, error) {
	body := map[string]any{}
	for k, v := range req.Raw {
		body[k] = v
	}
	body["model"] = req.Model
	body["messages"] = req.Messages
	body["stream"] = req.Stream
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	return body, nil
}
