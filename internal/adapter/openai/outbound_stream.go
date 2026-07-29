package openai

import (
	"encoding/json"
	"strings"

	"auto-router/internal/model"
)

// ParseSSELine parses one line of an OpenAI SSE stream.
// Returns (chunk, done, error). chunk is nil for blank/comment lines.
func ParseSSELine(line string) (*model.Chunk, bool, error) {
	if line == "" || strings.HasPrefix(line, ":") {
		return nil, false, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil, false, nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		return nil, true, nil
	}
	var ch model.Chunk
	if err := json.Unmarshal([]byte(data), &ch); err != nil {
		return nil, false, err
	}
	return &ch, false, nil
}

// EncodeChunk serializes a canonical chunk into an OpenAI SSE data line.
func EncodeChunk(ch *model.Chunk) ([]byte, error) {
	b, err := json.Marshal(ch)
	if err != nil {
		return nil, err
	}
	out := append([]byte("data: "), b...)
	return out, nil
}
