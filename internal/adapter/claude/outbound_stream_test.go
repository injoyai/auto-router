package claude

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestParseSSELine(t *testing.T) {
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\",\"model\":\"claude-3\",\"content\":[]}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	scanner := bufio.NewScanner(strings.NewReader(sse))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	var contents []string
	var finishReason string
	for scanner.Scan() {
		line := scanner.Text()
		ch, done, err := ParseSSELine(line)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if ch == nil {
			continue
		}
		if len(ch.Choices) > 0 {
			if ch.Choices[0].Delta.Content != "" {
				contents = append(contents, ch.Choices[0].Delta.Content)
			}
			if ch.Choices[0].FinishReason != "" {
				finishReason = ch.Choices[0].FinishReason
			}
		}
	}
	assert.Equal(t, []string{"Hel", "lo"}, contents)
	assert.Equal(t, "stop", finishReason)
}

func TestStreamEncoderText(t *testing.T) {
	enc := NewStreamEncoder("claude-3")

	// First chunk with content
	b1 := enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "Hello"}}},
	})
	s1 := string(b1)
	assert.Contains(t, s1, "message_start")
	assert.Contains(t, s1, "content_block_start")
	assert.Contains(t, s1, "content_block_delta")
	assert.Contains(t, s1, `"text":"Hello"`)

	// Second chunk
	b2 := enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "!"}}},
	})
	s2 := string(b2)
	assert.Contains(t, s2, "content_block_delta")
	assert.Contains(t, s2, `"text":"!"`)
	assert.NotContains(t, s2, "message_start") // only once

	// Finish
	b3 := enc.Finish()
	s3 := string(b3)
	assert.Contains(t, s3, "content_block_stop")
	assert.Contains(t, s3, "message_delta")
	assert.Contains(t, s3, "end_turn")
	assert.Contains(t, s3, "message_stop")
}

func TestStreamEncoderFinishReason(t *testing.T) {
	enc := NewStreamEncoder("claude-3")
	enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "Hi"}}},
	})
	b := enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{}, FinishReason: "tool_calls"}},
	})
	// FinishReason chunk should emit message_delta with tool_use
	s := string(b)
	assert.Contains(t, s, "message_delta")
	assert.Contains(t, s, "tool_use")
}

func TestStreamEncoderFinishWithoutContent(t *testing.T) {
	// Finish() called without any content chunks — should still emit message_start
	enc := NewStreamEncoder("claude-3")
	b := enc.Finish()
	s := string(b)
	assert.Contains(t, s, "message_start", "message_start must be emitted even with no content")
	assert.Contains(t, s, "message_stop")
}

func TestStreamEncoderFinishIdempotent(t *testing.T) {
	enc := NewStreamEncoder("claude-3")
	enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "Hi"}}},
	})
	b1 := enc.Finish()
	assert.NotEmpty(t, b1)
	// Second call should return nil (idempotent)
	b2 := enc.Finish()
	assert.Nil(t, b2, "Finish() should be idempotent")
}

func TestStreamEncoderEncodeAfterFinish(t *testing.T) {
	enc := NewStreamEncoder("claude-3")
	enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "Hi"}}},
	})
	enc.Finish()
	// EncodeChunk after Finish should return nil (terminal state)
	b := enc.EncodeChunk(&model.Chunk{
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "more"}}},
	})
	assert.Nil(t, b, "EncodeChunk after Finish should return nil")
}

func TestParseSSELineDoneSentinel(t *testing.T) {
	ch, done, err := ParseSSELine("data: [DONE]")
	assert.NoError(t, err)
	assert.Nil(t, ch)
	assert.True(t, done)
}

func TestParseSSELineNonTextDelta(t *testing.T) {
	// input_json_delta (tool-use streaming) should return nil chunk (known limitation)
	ch, done, err := ParseSSELine(`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`)
	assert.NoError(t, err)
	assert.False(t, done)
	assert.Nil(t, ch, "non-text_delta blocks should return nil chunk")
}
