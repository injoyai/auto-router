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
	GetSession(id string) (*store.Session, error)
	SetNextModel(id, model string, ttl time.Duration) error
}

// Compile-time guarantee that *store.Store satisfies StoreDeps.
var _ StoreDeps = (*store.Store)(nil)

// JudgeClient invokes the judge model to pick a model name.
type JudgeClient interface {
	Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error)
}

type Decision struct {
	ModelName string
	Model     *store.Model
	Reason    string // override | session | judge | fallback
	JudgeRaw  string
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

	// 2. Session next-model
	if req.SessionID != "" {
		if sess, err := e.Store.GetSession(req.SessionID); err == nil && sess != nil && sess.NextModel != "" {
			if m, err := e.Store.GetModelByName(sess.NextModel); err == nil && m != nil && m.Enabled {
				return &Decision{ModelName: m.Name, Model: m, Reason: "session"}, nil
			}
		}
	}

	// 3. Judge
	rc, err := e.Store.GetRoutingConfig()
	if err != nil {
		return nil, fmt.Errorf("get routing config: %w", err)
	}
	judge, _ := e.Store.GetJudgeModel()
	// I4: track judge diagnostics so fallback Decisions still carry JudgeRaw
	// for the request log, and log failures at warning level.
	judgeRaw := ""
	if judge != nil {
		cands, _ := e.Store.ListEnabledModels()
		userText := TruncateUserText(req.LastUserMessage(), rc.JudgeMaxInputChars)
		raw, jerr := e.Judge.Judge(judge, cands, userText)
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
					return &Decision{ModelName: m.Name, Model: m, Reason: "judge", JudgeRaw: raw}, nil
				}
			}
			log.Printf("[WARN] judge output unparseable: %q", raw)
			judgeRaw = raw
		}
	}

	// 4. Fallback to default model
	if rc.DefaultModelID != nil {
		if m, err := e.Store.GetModel(*rc.DefaultModelID); err == nil && m != nil {
			return &Decision{ModelName: m.Name, Model: m, Reason: "fallback", JudgeRaw: judgeRaw}, nil
		}
	}
	return nil, fmt.Errorf("no model available and no default configured")
}
