package claude

import (
	"encoding/json"
	"strings"

	"auto-router/internal/model"
)

// ParseSSELine parses one line of a Claude SSE stream.
// Returns (chunk, done, error). chunk is nil for non-data lines and for
// event types that don't map to canonical deltas (message_start,
// content_block_start, content_block_stop).
//
// Text streaming is fully supported. Tool-use streaming (input_json_delta)
// is not converted to canonical tool call deltas — tool calls work in
// non-streaming mode. This is a known limitation.
func ParseSSELine(line string) (*model.Chunk, bool, error) {
	if line == "" || strings.HasPrefix(line, ":") {
		return nil, false, nil
	}
	if strings.HasPrefix(line, "event:") {
		return nil, false, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil, false, nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		return nil, true, nil
	}

	var ev struct {
		Type  string          `json:"type"`
		Delta json.RawMessage `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil, false, err
	}

	switch ev.Type {
	case "content_block_delta":
		var d struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(ev.Delta, &d); err != nil {
			return nil, false, err
		}
		if d.Type == "text_delta" {
			return &model.Chunk{
				Choices: []model.ChunkChoice{{
					Index: 0,
					Delta: model.Delta{Content: d.Text},
				}},
			}, false, nil
		}
		return nil, false, nil

	case "message_delta":
		var d struct {
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal(ev.Delta, &d); err != nil {
			return nil, false, err
		}
		finish := stopReasonMap[d.StopReason]
		if finish == "" {
			finish = "stop"
		}
		return &model.Chunk{
			Choices: []model.ChunkChoice{{
				Index:        0,
				FinishReason: finish,
			}},
		}, false, nil

	case "message_stop":
		return nil, true, nil

	default:
		return nil, false, nil
	}
}

// StreamEncoder is a stateful encoder that converts canonical chunks into
// Claude SSE events. It tracks whether message_start and content_block_start
// have been emitted so they are only sent once.
type StreamEncoder struct {
	model          string
	messageStarted bool
	blockStarted   bool
	finishEmitted  bool
	stopped        bool
}

// NewStreamEncoder creates a new encoder for the given model name.
func NewStreamEncoder(modelName string) *StreamEncoder {
	return &StreamEncoder{model: modelName}
}

// EncodeChunk converts a canonical chunk to Claude SSE events (as bytes).
// Returns the full SSE text including event/data lines and \n\n separators.
// Returns nil if the chunk produces no output.
func (e *StreamEncoder) EncodeChunk(ch *model.Chunk) []byte {
	if e.finishEmitted {
		return nil // terminal state: refuse to encode after finish
	}
	if ch == nil || len(ch.Choices) == 0 {
		return nil
	}
	choice := ch.Choices[0]
	var buf strings.Builder

	// If finish_reason is set, emit message_delta (but not message_stop yet —
	// that's in Finish())
	if choice.FinishReason != "" {
		sr := finishReasonToClaude[choice.FinishReason]
		if sr == "" {
			sr = "end_turn"
		}
		if !e.finishEmitted {
			e.finishEmitted = true
			e.ensureMessageStarted(&buf)
			e.ensureBlockClosed(&buf)
			ev := map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": sr},
			}
			writeSSEEvent(&buf, "message_delta", ev)
		}
		return []byte(buf.String())
	}

	if choice.Delta.Content == "" {
		return nil
	}

	// Ensure message_start + content_block_start are emitted
	if !e.messageStarted {
		e.messageStarted = true
		msgStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":          "msg_router",
				"type":        "message",
				"role":        "assistant",
				"model":       e.model,
				"content":     []any{},
				"stop_reason": nil,
				"usage":       map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		}
		writeSSEEvent(&buf, "message_start", msgStart)
	}
	if !e.blockStarted {
		e.blockStarted = true
		blockStart := map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "text", "text": ""},
		}
		writeSSEEvent(&buf, "content_block_start", blockStart)
	}

	// Emit content_block_delta
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "text_delta", "text": choice.Delta.Content},
	}
	writeSSEEvent(&buf, "content_block_delta", delta)

	return []byte(buf.String())
}

// Finish emits terminal events: content_block_stop, message_delta (if not
// already emitted), and message_stop.
func (e *StreamEncoder) Finish() []byte {
	if e.stopped {
		return nil
	}
	e.stopped = true
	var buf strings.Builder
	e.ensureMessageStarted(&buf)
	e.ensureBlockClosed(&buf)
	if !e.finishEmitted {
		e.finishEmitted = true
		writeSSEEvent(&buf, "message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
		})
	}
	writeSSEEvent(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return []byte(buf.String())
}

func (e *StreamEncoder) ensureBlockClosed(buf *strings.Builder) {
	if e.blockStarted {
		e.blockStarted = false
		writeSSEEvent(buf, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
	}
}

// ensureMessageStarted emits message_start if it has not been emitted yet.
// Required by the Claude SSE protocol as the first event of a stream, even
// when no content was produced (e.g. empty upstream response or Finish()
// called without prior content).
func (e *StreamEncoder) ensureMessageStarted(buf *strings.Builder) {
	if !e.messageStarted {
		e.messageStarted = true
		writeSSEEvent(buf, "message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":          "msg_router",
				"type":        "message",
				"role":        "assistant",
				"model":       e.model,
				"content":     []any{},
				"stop_reason": nil,
				"usage":       map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		})
	}
}

// writeSSEEvent writes an event: + data: pair to the buffer.
func writeSSEEvent(buf *strings.Builder, eventType string, data map[string]any) {
	b, _ := json.Marshal(data)
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(b)
	buf.WriteString("\n\n")
}

// StreamParser is a stateful SSE parser for Claude streams. It tracks the
// current event type and input_tokens (from message_start) so that the
// final message_delta chunk can emit a complete Usage.
type StreamParser struct {
	model        string
	currentEvent string
	inputTokens  int
	cacheTokens  int
}

// NewStreamParser creates a StreamParser for the given model name.
func NewStreamParser(model string) *StreamParser {
	return &StreamParser{model: model}
}

// Parse processes one SSE line and returns (chunk, done, error).
func (p *StreamParser) Parse(line string) (*model.Chunk, bool, error) {
	if line == "" || strings.HasPrefix(line, ":") {
		return nil, false, nil
	}
	if strings.HasPrefix(line, "event:") {
		p.currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return nil, false, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil, false, nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

	var evt struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &evt); err != nil {
		return nil, false, err
	}

	switch evt.Type {
	case "message_start":
		var ms struct {
			Message struct {
				Usage struct {
					InputTokens          int `json:"input_tokens"`
					OutputTokens         int `json:"output_tokens"`
					CacheReadInputTokens int `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &ms); err != nil {
			return nil, false, err
		}
		p.inputTokens = ms.Message.Usage.InputTokens
		p.cacheTokens = ms.Message.Usage.CacheReadInputTokens
		return nil, false, nil

	case "content_block_delta":
		var cd struct {
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &cd); err != nil {
			return nil, false, err
		}
		return &model.Chunk{
			Model:   p.model,
			Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: cd.Delta.Text}}},
		}, false, nil

	case "message_delta":
		var md struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &md); err != nil {
			return nil, false, err
		}
		finishReason := stopReasonMap[md.Delta.StopReason]
		if finishReason == "" {
			finishReason = "stop"
		}
		return &model.Chunk{
			Model: p.model,
			Choices: []model.ChunkChoice{{
				Index:        0,
				Delta:        model.Delta{},
				FinishReason: finishReason,
			}},
			Usage: &model.Usage{
				PromptTokens:     p.inputTokens,
				CompletionTokens: md.Usage.OutputTokens,
				TotalTokens:      p.inputTokens + md.Usage.OutputTokens,
				CacheTokens:      p.cacheTokens,
			},
		}, false, nil

	case "message_stop":
		return nil, true, nil
	}

	return nil, false, nil
}
