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
