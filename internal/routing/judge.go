package routing

import (
	"strings"

	"auto-router/internal/model"
)

// Candidate is a routable candidate (a queue) presented to the judge.
type Candidate struct {
	Name        string
	Description string
}

const judgeSystemPrompt = `你是一个模型路由器。根据用户任务和可用队列列表,选择最合适的队列。
回复格式(共三行,不要输出额外内容):
- 第一行:队列名称(必须与列表中的某个名称完全一致)
- 第二行:[任务] 一句话总结用户在做什么任务
- 第三行:[理由] 一句话说明为什么选择该队列`

// BuildJudgeMessages constructs the messages to send to the judge model.
func BuildJudgeMessages(candidates []Candidate, userText string) []model.Message {
	var sb strings.Builder
	sb.WriteString("可用队列列表:\n")
	for _, c := range candidates {
		sb.WriteString("- ")
		sb.WriteString(c.Name)
		if c.Description != "" {
			sb.WriteString(" - ")
			sb.WriteString(c.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n用户任务:\n")
	sb.WriteString(userText)
	return []model.Message{
		{Role: "system", Content: judgeSystemPrompt},
		{Role: "user", Content: sb.String()},
	}
}

// ParseJudgeOutput normalizes the judge model's reply and matches it against
// known model names. Returns "" if no match.
//
// The judge is asked to put the model name on the first line and a brief
// reason on subsequent lines. We prefer the first line for an exact match so
// that a reason mentioning another model's name cannot cause a mismatch.
func ParseJudgeOutput(raw string, known []string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "`\"' \n")
	// strip markdown fence like ```json ... ```
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// Prefer the first line as the model name (format: name + reason).
	first := s
	if i := strings.Index(s, "\n"); i >= 0 {
		first = strings.TrimSpace(s[:i])
	}
	for _, k := range known {
		if first == k {
			return k
		}
	}
	// full-text exact match (single-line replies)
	for _, k := range known {
		if s == k {
			return k
		}
	}
	// fallback: case-insensitive contains
	low := strings.ToLower(s)
	for _, k := range known {
		if strings.Contains(low, strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

// TruncateUserText caps user input length for the judge prompt.
func TruncateUserText(s string, maxChars int) string {
	if maxChars <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	return string(r[:maxChars])
}
