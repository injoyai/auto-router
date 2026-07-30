package openai

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestScanChunks(t *testing.T) {
	sse := "data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n" +
		"data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	scanner := bufio.NewScanner(strings.NewReader(sse))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	var contents []string
	var finish string
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
			contents = append(contents, ch.Choices[0].Delta.Content)
			if ch.Choices[0].FinishReason != "" {
				finish = ch.Choices[0].FinishReason
			}
		}
	}
	assert.Equal(t, []string{"hel", "lo"}, contents)
	assert.Equal(t, "stop", finish)
}

func TestEncodeChunk(t *testing.T) {
	ch := &model.Chunk{
		Model: "gpt-4",
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "x"}}},
	}
	b, err := EncodeChunk(ch)
	assert.NoError(t, err)
	assert.Equal(t, `data: {"model":"gpt-4","choices":[{"index":0,"delta":{"content":"x"}}]}`, string(b))
}

func TestParseSSELineWithUsage(t *testing.T) {
	// OpenAI 流式最终 chunk 携带 usage
	line := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`
	ch, done, err := ParseSSELine(line)
	assert.NoError(t, err)
	assert.False(t, done)
	assert.NotNil(t, ch, "chunk should not be nil")
	assert.NotNil(t, ch.Usage, "Usage should be parsed")
	assert.Equal(t, 10, ch.Usage.PromptTokens)
	assert.Equal(t, 20, ch.Usage.CompletionTokens)
	assert.Equal(t, 30, ch.Usage.TotalTokens)
}

func TestParseSSELineNoUsage(t *testing.T) {
	// 普通 delta chunk 没有 usage
	line := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`
	ch, _, err := ParseSSELine(line)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	assert.Nil(t, ch.Usage, "Usage should be nil for non-final chunk")
}
