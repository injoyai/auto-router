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
	GetRoutingConfig() (*store.RoutingConfig, error)
	ListEnabledModelGroups() ([]store.ModelGroup, error)
	GetModelGroup(id uint) (*store.ModelGroup, error)
	GetModelGroupByName(name string) (*store.ModelGroup, error)
	GetGroupChain(groupID uint) ([]store.Model, error)
}

// Compile-time guarantee that *store.Store satisfies StoreDeps.
var _ StoreDeps = (*store.Store)(nil)

// JudgeClient invokes the judge queue to pick a queue name.
// Judge receives the ordered judge chain and returns:
//   - raw:       the judge model's textual output
//   - servedModel: the name of the judge model that actually succeeded
//                  (after failover within the chain)
//   - usage:     token usage of the successful judge call (nil on failure)
//   - trace:     per-model attempt history of the judge chain
//   - err:       non-nil when the whole judge chain is exhausted
type JudgeClient interface {
	Judge(chain []*store.Model, candidates []Candidate, userText string) (raw string, servedModel string, usage *model.Usage, trace []store.Attempt, err error)
}

type Decision struct {
	ModelName     string         // target name (queue name), used for RoutedModel
	Model         *store.Model   // first model in chain (back-compat)
	Models        []*store.Model // ordered chain
	Reason        string         // override | judge | fallback
	ServedModel   string         // filled by gateway on success
	FailoverCount int            // filled by gateway
	Trace         []store.Attempt // filled by gateway: model queue attempts
	JudgeTrace    []store.Attempt // filled by engine: judge attempts
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

// toPtrChain converts a slice of Model values to a slice of pointers,
// preserving order. Each pointer addresses a distinct element.
func toPtrChain(chain []store.Model) []*store.Model {
	out := make([]*store.Model, len(chain))
	for i := range chain {
		out[i] = &chain[i]
	}
	return out
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
	return toPtrChain(chain), nil
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

	// 2. Judge: candidates are only enabled queues with non-empty chains,
	// excluding the judge queue itself (it is for judging, not for serving).
	rc, err := e.Store.GetRoutingConfig()
	if err != nil {
		return nil, fmt.Errorf("get routing config: %w", err)
	}
	judgeName := ""
	judgeRaw := ""
	var judgeUsage *model.Usage
	var judgeLatency time.Duration
	var judgeTrace []store.Attempt

	var judgeChain []*store.Model
	if rc.JudgeGroupID != nil {
		if g, gerr := e.Store.GetModelGroup(*rc.JudgeGroupID); gerr == nil && g != nil && g.Enabled {
			if ch, cerr := e.Store.GetGroupChain(g.ID); cerr == nil && len(ch) > 0 {
				judgeChain = toPtrChain(ch)
			}
		}
	}

	if len(judgeChain) > 0 {
		groups, _ := e.Store.ListEnabledModelGroups()
		cands := make([]Candidate, 0, len(groups))
		known := make([]string, 0, len(groups))
		for _, g := range groups {
			if rc.JudgeGroupID != nil && g.ID == *rc.JudgeGroupID {
				continue // exclude the judge queue itself
			}
			ch, err := e.Store.GetGroupChain(g.ID)
			if err != nil || len(ch) == 0 {
				continue
			}
			cands = append(cands, Candidate{Name: g.Name, Description: g.Remark})
			known = append(known, g.Name)
		}
		userText := req.AllUserMessages()
		jStart := time.Now()
		raw, servedName, usage, jTrace, jerr := e.Judge.Judge(judgeChain, cands, userText)
		judgeLatency = time.Since(jStart)
		judgeUsage = usage
		judgeName = servedName
		judgeTrace = jTrace
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
					return &Decision{ModelName: picked, Model: chain[0], Models: chain, Reason: "judge", JudgeRaw: raw, JudgeModel: servedName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency, JudgeTrace: judgeTrace}, nil
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
				out := toPtrChain(chain)
				return &Decision{ModelName: g.Name, Model: out[0], Models: out, Reason: "fallback", JudgeRaw: judgeRaw, JudgeModel: judgeName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency, JudgeTrace: judgeTrace}, nil
			}
		}
	}
	return nil, fmt.Errorf("no queue available and no default configured")
}
