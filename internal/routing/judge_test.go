package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/store"
)

func TestBuildJudgeMessages(t *testing.T) {
	candidates := []store.Model{
		{Name: "gpt-4o-mini", DisplayName: "Fast", Description: "small fast"},
		{Name: "gpt-4o", DisplayName: "Smart", Description: "large smart"},
	}
	msgs := BuildJudgeMessages(candidates, "Write a haiku")
	assert.Equal(t, "system", msgs[0].Role)
	assert.Contains(t, msgs[0].Content, "模型路由器")
	assert.Equal(t, "user", msgs[1].Role)
	assert.Contains(t, msgs[1].Content, "gpt-4o-mini")
	assert.Contains(t, msgs[1].Content, "gpt-4o")
	assert.Contains(t, msgs[1].Content, "Write a haiku")
}

func TestParseJudgeOutput(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":               "gpt-4o",
		"  gpt-4o  ":           "gpt-4o",
		"```gpt-4o```":         "gpt-4o",
		"```json\ngpt-4o\n```": "gpt-4o",
		"\"gpt-4o\"":           "gpt-4o",
		"nonsense":             "",
	}
	for in, want := range cases {
		got := ParseJudgeOutput(in, []string{"gpt-4o"})
		assert.Equal(t, want, got, "input %q", in)
	}
}

func TestTruncateUserText(t *testing.T) {
	long := ""
	for i := 0; i < 5000; i++ {
		long += "x"
	}
	out := TruncateUserText(long, 100)
	assert.Equal(t, 100, len(out))
}
