package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractNextModel(t *testing.T) {
	text := "Here is your answer.<<next_model: gpt-4o>>"
	clean, model := ExtractNextModel(text)
	assert.Equal(t, "Here is your answer.", clean)
	assert.Equal(t, "gpt-4o", model)
}

func TestExtractNextModelNone(t *testing.T) {
	clean, model := ExtractNextModel("no directive here")
	assert.Equal(t, "no directive here", clean)
	assert.Equal(t, "", model)
}

func TestExtractNextModelWhitespace(t *testing.T) {
	clean, model := ExtractNextModel("ans<<next_model:   gpt-4  >>")
	assert.Equal(t, "ans", clean)
	assert.Equal(t, "gpt-4", model)
}
