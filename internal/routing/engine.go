package routing

import (
	"fmt"
	"log"
	"time"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

// StoreDeps is the subset of *store.Store the engine needs.
type StoreDeps interface {
	GetJudgeModel() (*store.Model, error)
	GetRoutingConfig() (*store.RoutingConfig, error)
	GetModel(id uint) (*store.Model, error)
	GetModelByName(name string) (*store.Model, error)
	ListEnabledModels() ([]store.Model, error)
}

// Compile-time guarantee that *store.Store satisfies StoreDeps.
var _ StoreDeps = (*store.Store)(nil)

// JudgeClient invokes the judge model to pick a model name.
// Returns (content, usage, err): usage is the token usage of the judge call
// (nil when the judge was not invoked or the call failed before parsing).
type JudgeClient interface {
	Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, *model.Usage, error)
}

type Decision struct {
	ModelName   string
	Model       *store.Model
	Reason      string // override | judge | fallback
	JudgeRaw    string
	JudgeModel  string        // name of the judge model (empty if judge not invoked)
	JudgeUsage  *model.Usage   // token usage of the judge call (nil if not called)
	JudgeLatency time.Duration  // elapsed time of the judge call
}

type Engine struct {
	Store StoreDeps
	Judge JudgeClient
}

func New(s StoreDeps, j JudgeClient) *Engine {
	return &Engine{Store: s, Judge: j}
}

// Route decides which model to use for the request.
func (e *Engine) Route(req *model.ChatRequest) (*Decision, error) {
	// 1. Override
	if req.Override != "" {
		if m, err := e.Store.GetModelByName(req.Override); err == nil && m != nil && m.Enabled {
			return &Decision{ModelName: m.Name, Model: m, Reason: "override"}, nil
		}
	}

	// 2. Judge
	rc, err := e.Store.GetRoutingConfig()
	if err != nil {
		return nil, fmt.Errorf("get routing config: %w", err)
	}
	judge, _ := e.Store.GetJudgeModel()
	// I4: track judge diagnostics so fallback Decisions still carry JudgeRaw
	// for the request log, and log failures at warning level.
	judgeName := ""
	judgeRaw := ""
	var judgeUsage *model.Usage
	var judgeLatency time.Duration
	if judge != nil {
		judgeName = judge.Name
		cands, _ := e.Store.ListEnabledModels()
		userText := TruncateUserText(req.LastUserMessage(), rc.JudgeMaxInputChars)
		jStart := time.Now()
		raw, usage, jerr := e.Judge.Judge(judge, cands, userText)
		judgeLatency = time.Since(jStart)
		judgeUsage = usage
		switch {
		case jerr != nil:
			log.Printf("[WARN] judge call failed: %v", jerr)
			judgeRaw = "error: " + jerr.Error()
		case raw == "":
			log.Printf("[WARN] judge returned empty output")
			judgeRaw = "error: empty judge output"
		default:
			known := make([]string, 0, len(cands))
			for _, c := range cands {
				known = append(known, c.Name)
			}
			if picked := ParseJudgeOutput(raw, known); picked != "" {
				if m, err := e.Store.GetModelByName(picked); err == nil && m != nil {
					return &Decision{ModelName: m.Name, Model: m, Reason: "judge", JudgeRaw: raw, JudgeModel: judgeName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency}, nil
				}
			}
			log.Printf("[WARN] judge output unparseable: %q", raw)
			judgeRaw = raw
		}
	}

	// 3. Fallback to default model
	if rc.DefaultModelID != nil {
		if m, err := e.Store.GetModel(*rc.DefaultModelID); err == nil && m != nil {
			return &Decision{ModelName: m.Name, Model: m, Reason: "fallback", JudgeRaw: judgeRaw, JudgeModel: judgeName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency}, nil
		}
	}
	return nil, fmt.Errorf("no model available and no default configured")
}
