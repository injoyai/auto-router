package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseResponse(t *testing.T) {
	raw := `{"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)

	resp, err := ParseResponse(m)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", resp.Model)
	assert.Len(t, resp.Choices, 1)
	assert.Equal(t, "hello", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Equal(t, 3, resp.Usage.TotalTokens)
}
