package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRequest(t *testing.T) {
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"stream":true,"temperature":0.5}`
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)

	req, err := ParseRequest(raw)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", req.Model)
	assert.Len(t, req.Messages, 1)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "hi", req.Messages[0].Content)
	assert.True(t, req.Stream)
	assert.Equal(t, 0.5, raw["temperature"]) // passthrough preserved (json numbers -> float64)
}

func TestParseRequestOverrideSentinel(t *testing.T) {
	for _, m := range []string{"", "auto", "route"} {
		req, _ := ParseRequest(map[string]any{"model": m})
		assert.True(t, req.IsRouteRequested(), "model=%q should trigger routing", m)
	}
}

func TestParseRequestExplicitModel(t *testing.T) {
	req, _ := ParseRequest(map[string]any{"model": "gpt-4"})
	assert.False(t, req.IsRouteRequested())
}
