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

const judgeSystemPrompt = `你是一个模型路由器。根据用户当前这一步请求的意图,判断它属于哪个阶段,并选择最合适的队列。

阶段判断原则(参考软件开发生命周期):

【需要强推理模型的阶段】
- 需求分析:把模糊想法变成具体需求,用户在描述问题、探索方向、澄清目标
- 架构设计:设计技术方案、选型、模块划分、接口设计、任务拆解、数据结构设计
- 调试分析:分析 bug 原因、定位问题、排查错误、性能分析、日志解读
- 代码评审:审查代码质量、发现潜在问题、安全审计、风险评估
- 重构简化:在保留行为的前提下降低复杂度、优化结构、消除技术债
- 长文本推理:阅读长文档、理解复杂逻辑、多文件关联分析、方案对比

【普通或快速模型即可的阶段】
- 编码实现:写具体代码、补全函数、实现功能、CRUD 操作
- 修复 bug:按照明确指示修改 bug、小范围改动、替换实现
- 测试编写:写单元测试、集成测试、测试用例
- 文档编写:写注释、README、API 文档、变更日志
- 简单问答:格式转换、命名建议、简单咨询、知识查询
- 配置部署:改配置、CI/CD、环境搭建、脚本编写

判断要点:
1. 不要只看任务整体目标(如"做一个俄罗斯方块"),而要看当前这一步用户到底在要什么
2. 同一个项目不同步骤可能选不同队列:先设计方案用强模型,后写代码用普通模型
3. 当用户在"思考/分析/设计/评审"时选强模型,当用户在"执行/编写/修改/转换"时选普通模型
4. 看动词判断意图:"设计/分析/评估/排查"多为推理类,"实现/编写/修改/补充"多为执行类

回复格式(共三行,不要输出额外内容):
- 第一行:队列名称(必须与列表中的某个名称完全一致)
- 第二行:[阶段] 一句话说明当前这一步属于什么阶段
- 第三行:[理由] 一句话说明为什么选这个队列`

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
