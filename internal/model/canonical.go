package model

import "strings"

// ChatRequest is the internal canonical request (OpenAI-based).
type ChatRequest struct {
	Model     string
	Messages  []Message
	Tools     []Tool
	Stream    bool
	SessionID string
	Override  string // explicit model override (X-Route-Model or model field)
	ClientFmt string // openai | claude — which protocol the client used
	// raw JSON of client body, kept for fields we pass through unchanged
	Raw map[string]any `json:"-"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Tool struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

// LastUserMessage returns the content of the last user message, or "".
func (r *ChatRequest) LastUserMessage() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			return r.Messages[i].Content
		}
	}
	return ""
}

// AllUserMessages concatenates all user messages in chronological order,
// separated by newlines. Used by the judge to see the full task context
// (not just the last message, which may be a short follow-up like "继续").
func (r *ChatRequest) AllUserMessages() string {
	var parts []string
	for _, m := range r.Messages {
		if m.Role == "user" && m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// IsRouteRequested reports whether the client wants auto-routing (no explicit model).
// Sentinel values "", "auto", "route" trigger routing; any other model name is an
// explicit override. Defined here (in package model) because Go does not allow
// defining methods on imported types from another package.
func (r *ChatRequest) IsRouteRequested() bool {
	m := r.Model
	return m == "" || m == "auto" || m == "route"
}

// ChatResponse is the canonical non-streaming response.
type ChatResponse struct {
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Delta is one streaming chunk (OpenAI-style).
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Chunk struct {
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

type ChunkChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}
