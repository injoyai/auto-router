package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildJudgeMessages(t *testing.T) {
	candidates := []Candidate{
		{Name: "deepseek-v4-flash", Description: "fast"},
		{Name: "gpt-4o-pro", Description: "smart"},
	}
	msgs := BuildJudgeMessages(candidates, "Write a haiku")
	assert.Equal(t, "system", msgs[0].Role)
	assert.Contains(t, msgs[0].Content, "模型路由器")
	assert.Equal(t, "user", msgs[1].Role)
	assert.Contains(t, msgs[1].Content, "deepseek-v4-flash")
	assert.Contains(t, msgs[1].Content, "gpt-4o-pro")
	assert.Contains(t, msgs[1].Content, "Write a haiku")
}

// TestBuildJudgeMessagesFormatReminder verifies the user message ends with a
// format reminder so that judge models (especially Flash-tier) don't drift
// into tool-call/markup hallucinations on long inputs.
func TestBuildJudgeMessagesFormatReminder(t *testing.T) {
	msgs := BuildJudgeMessages([]Candidate{{Name: "q1"}}, "hello")
	assert.Contains(t, msgs[1].Content, "请按格式回复三行")
	assert.Contains(t, msgs[1].Content, "不要调用工具")
}

func TestParseJudgeOutput(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":               "gpt-4o",
		"  gpt-4o  ":           "gpt-4o",
		"```gpt-4o```":         "gpt-4o",
		"```json\ngpt-4o\n```": "gpt-4o",
		"\"gpt-4o\"":           "gpt-4o",
		"nonsense":             "",
		// name + reason format: first line is the model name
		"gpt-4o\n适合代码任务":               "gpt-4o",
		"gpt-4o\n代码任务,claude-3 也适合但更贵": "gpt-4o",
		// name + task + reason format: only the first line is matched
		"gpt-4o\n[任务] 写 Python 函数\n[理由] 代码任务,gpt-4o 擅长,claude-3 也可以": "gpt-4o",
	}
	for in, want := range cases {
		got := ParseJudgeOutput(in, []string{"gpt-4o", "claude-3"})
		assert.Equal(t, want, got, "input %q", in)
	}
}
