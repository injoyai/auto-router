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
	"tool_choice", "parallel_tool_calls", "stream_options", "logit_bias",
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
	// Same-protocol (openai→openai): passthrough the original messages so
	// multimodal content (image_url blocks etc.) is preserved. The canonical
	// string form would drop non-text blocks. Cross-protocol clients (Claude)
	// use the rebuilt canonical messages to avoid leaking Claude-specific shapes.
	if req.ClientFmt == "openai" {
		if rawMsgs, ok := req.Raw["messages"]; ok {
			body["messages"] = rawMsgs
		} else {
			body["messages"] = req.Messages
		}
	} else {
		body["messages"] = req.Messages
	}
	body["stream"] = req.Stream
	// For streaming requests, ensure the upstream returns usage in the final
	// chunk. Most OpenAI-compatible APIs (including DeepSeek) omit usage
	// unless stream_options.include_usage is true. If the client already set
	// stream_options we respect it; otherwise we inject it.
	if req.Stream {
		if _, ok := body["stream_options"]; !ok {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	return body, nil
}
