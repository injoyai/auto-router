package routing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

type fakeStore struct {
	judgeGroup *uint
	defGroup   *uint
	groups     []store.ModelGroup
	chains     map[uint][]store.Model
	byName     map[string]*store.ModelGroup
}

func (f *fakeStore) GetRoutingConfig() (*store.RoutingConfig, error) {
	return &store.RoutingConfig{JudgeGroupID: f.judgeGroup, DefaultGroupID: f.defGroup}, nil
}
func (f *fakeStore) ListEnabledModelGroups() ([]store.ModelGroup, error) { return f.groups, nil }
func (f *fakeStore) GetModelGroup(id uint) (*store.ModelGroup, error) {
	for _, g := range f.groups {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeStore) GetModelGroupByName(n string) (*store.ModelGroup, error) {
	if g, ok := f.byName[n]; ok {
		return g, nil
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeStore) GetGroupChain(groupID uint) ([]store.Model, error) { return f.chains[groupID], nil }

type fakeJudge struct {
	out string
	err error
}

func (fj *fakeJudge) Judge(chain []*store.Model, candidates []Candidate, userText string) (string, string, *model.Usage, error) {
	served := ""
	if len(chain) > 0 {
		served = chain[0].Name
	}
	return fj.out, served, nil, fj.err
}

func puint(v uint) *uint { return &v }

func newEngine() (*Engine, *fakeStore, *fakeJudge) {
	jm := store.Model{ID: 9, Name: "judge-mini", Enabled: true}
	m := store.Model{ID: 1, Name: "gpt-4o", Enabled: true}
	g := store.ModelGroup{ID: 7, Name: "deepseek-v4-flash", Enabled: true}
	jg := store.ModelGroup{ID: 8, Name: "judge", Enabled: true}
	fs := &fakeStore{
		groups: []store.ModelGroup{g, jg},
		byName: map[string]*store.ModelGroup{"deepseek-v4-flash": &g, "judge": &jg},
		chains: map[uint][]store.Model{7: {m}, 8: {jm}},
	}
	fj := &fakeJudge{out: "deepseek-v4-flash"}
	return New(fs, fj), fs, fj
}

func TestRouteOverride(t *testing.T) {
	e, _, _ := newEngine()
	dec, err := e.Route(&model.ChatRequest{Override: "deepseek-v4-flash"})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "override", dec.Reason)
	assert.Len(t, dec.Models, 1)
}

func TestRouteOverrideUnknownQueue(t *testing.T) {
	e, _, _ := newEngine()
	_, err := e.Route(&model.ChatRequest{Override: "no-such-queue"})
	assert.Error(t, err)
}

func TestRouteJudge(t *testing.T) {
	e, fs, _ := newEngine()
	fs.judgeGroup = puint(8) // judge queue
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "judge", dec.Reason)
	assert.Equal(t, "judge-mini", dec.JudgeModel)
}

func TestRouteFallbackOnBadJudge(t *testing.T) {
	e, fs, fj := newEngine()
	fs.judgeGroup = puint(8)
	fj.out = "nonexistent"
	fs.defGroup = puint(7)
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "fallback", dec.Reason)
}

func TestRouteOverrideMultiModelChainOrder(t *testing.T) {
	m1 := store.Model{ID: 1, Name: "m1", Enabled: true}
	m2 := store.Model{ID: 2, Name: "m2", Enabled: true}
	g := store.ModelGroup{ID: 8, Name: "multi-q", Enabled: true}
	fs := &fakeStore{
		groups: []store.ModelGroup{g},
		byName: map[string]*store.ModelGroup{"multi-q": &g},
		chains: map[uint][]store.Model{8: {m1, m2}},
	}
	e := New(fs, &fakeJudge{})
	dec, err := e.Route(&model.ChatRequest{Override: "multi-q"})
	assert.NoError(t, err)
	assert.Equal(t, "multi-q", dec.ModelName)
	assert.Len(t, dec.Models, 2)
	assert.Equal(t, "m1", dec.Models[0].Name)
	assert.Equal(t, "m2", dec.Models[1].Name)
	assert.Equal(t, "m1", dec.Model.Name) // chain head
}
