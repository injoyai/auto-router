package routing

import (
	"strings"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

const judgeSystemPrompt = `你是一个模型路由器。根据用户任务和可用模型列表,选择最合适的模型。只回复模型名称,不要解释。`

// BuildJudgeMessages constructs the messages to send to the judge model.
func BuildJudgeMessages(candidates []store.Model, userText string) []model.Message {
	var sb strings.Builder
	sb.WriteString("可用模型列表:\n")
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
