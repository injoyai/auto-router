package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLastUserMessage(t *testing.T) {
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "s"},
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "a"},
			{Role: "user", Content: "second"},
		},
	}
	assert.Equal(t, "second", req.LastUserMessage())
}

func TestLastUserMessageEmpty(t *testing.T) {
	req := &ChatRequest{}
	assert.Equal(t, "", req.LastUserMessage())
}
