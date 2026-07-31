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
	ListEnabledModelGroups() ([]store.ModelGroup, error)
	GetModelGroup(id uint) (*store.ModelGroup, error)
	GetModelGroupByName(name string) (*store.ModelGroup, error)
	GetGroupChain(groupID uint) ([]store.Model, error)
}

// Compile-time guarantee that *store.Store satisfies StoreDeps.
var _ StoreDeps = (*store.Store)(nil)

// JudgeClient invokes the judge model to pick a queue name.
// Returns (content, usage, err): usage is the token usage of the judge call
// (nil when the judge was not invoked or the call failed before parsing).
type JudgeClient interface {
	Judge(judgeModel *store.Model, candidates []Candidate, userText string) (string, *model.Usage, error)
}

type Decision struct {
	ModelName     string         // target name (queue name), used for RoutedModel
	Model         *store.Model   // first model in chain (back-compat)
	Models        []*store.Model // ordered chain
	Reason        string         // override | judge | fallback
	ServedModel   string         // filled by gateway on success
	FailoverCount int            // filled by gateway
	JudgeRaw      string
	JudgeModel    string
	JudgeUsage    *model.Usage
	JudgeLatency  time.Duration
}

type Engine struct {
	Store StoreDeps
	Judge JudgeClient
}

func New(s StoreDeps, j JudgeClient) *Engine {
	return &Engine{Store: s, Judge: j}
}

// resolveGroupChain resolves a queue name to an ordered model chain.
// Only looks up ModelGroup, never Model.
func (e *Engine) resolveGroupChain(name string) ([]*store.Model, error) {
	g, err := e.Store.GetModelGroupByName(name)
	if err != nil || g == nil || !g.Enabled {
		return nil, fmt.Errorf("queue %q not found", name)
	}
	chain, err := e.Store.GetGroupChain(g.ID)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("queue %q has no available models", name)
	}
	out := make([]*store.Model, len(chain))
	for i := range chain {
		out[i] = &chain[i]
	}
	return out, nil
}

// Route decides which queue to use for the request.
func (e *Engine) Route(req *model.ChatRequest) (*Decision, error) {
	// 1. Override: must be a queue name; miss -> error (no fallback)
	if req.Override != "" {
		chain, err := e.resolveGroupChain(req.Override)
		if err != nil {
			return nil, err
		}
		return &Decision{ModelName: req.Override, Model: chain[0], Models: chain, Reason: "override"}, nil
	}

	// 2. Judge: candidates are only enabled queues with non-empty chains
	rc, err := e.Store.GetRoutingConfig()
	if err != nil {
		return nil, fmt.Errorf("get routing config: %w", err)
	}
	judge, _ := e.Store.GetJudgeModel()
	judgeName := ""
	judgeRaw := ""
	var judgeUsage *model.Usage
	var judgeLatency time.Duration
	if judge != nil {
		judgeName = judge.Name
		groups, _ := e.Store.ListEnabledModelGroups()
		cands := make([]Candidate, 0, len(groups))
		known := make([]string, 0, len(groups))
		for _, g := range groups {
			ch, err := e.Store.GetGroupChain(g.ID)
			if err != nil || len(ch) == 0 {
				continue
			}
			cands = append(cands, Candidate{Name: g.Name, Description: g.Description})
			known = append(known, g.Name)
		}
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
			if picked := ParseJudgeOutput(raw, known); picked != "" {
				if chain, err := e.resolveGroupChain(picked); err == nil {
					return &Decision{ModelName: picked, Model: chain[0], Models: chain, Reason: "judge", JudgeRaw: raw, JudgeModel: judgeName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency}, nil
				}
			}
			log.Printf("[WARN] judge output unparseable: %q", raw)
			judgeRaw = raw
		}
	}

	// 3. Fallback: default group
	if rc.DefaultGroupID != nil {
		if g, err := e.Store.GetModelGroup(*rc.DefaultGroupID); err == nil && g != nil && g.Enabled {
			if chain, err := e.Store.GetGroupChain(g.ID); err == nil && len(chain) > 0 {
				out := make([]*store.Model, len(chain))
				for i := range chain {
					out[i] = &chain[i]
				}
				return &Decision{ModelName: g.Name, Model: out[0], Models: out, Reason: "fallback", JudgeRaw: judgeRaw, JudgeModel: judgeName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency}, nil
			}
		}
	}
	return nil, fmt.Errorf("no queue available and no default configured")
}
