package routing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

type fakeStore struct {
	judge    *store.Model
	defGroup *uint
	groups   []store.ModelGroup
	chains   map[uint][]store.Model
	byName   map[string]*store.ModelGroup
}

func (f *fakeStore) GetJudgeModel() (*store.Model, error) { return f.judge, nil }
func (f *fakeStore) GetRoutingConfig() (*store.RoutingConfig, error) {
	return &store.RoutingConfig{DefaultGroupID: f.defGroup, JudgeMaxInputChars: 1000}, nil
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

func (fj *fakeJudge) Judge(judgeModel *store.Model, candidates []Candidate, userText string) (string, *model.Usage, error) {
	return fj.out, nil, fj.err
}

func puint(v uint) *uint { return &v }

func newEngine() (*Engine, *fakeStore, *fakeJudge) {
	m := store.Model{ID: 1, Name: "gpt-4o", Enabled: true}
	g := store.ModelGroup{ID: 7, Name: "deepseek-v4-flash", Enabled: true}
	fs := &fakeStore{
		groups: []store.ModelGroup{g},
		byName: map[string]*store.ModelGroup{"deepseek-v4-flash": &g},
		chains: map[uint][]store.Model{7: {m}},
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
	fs.judge = &store.Model{ID: 9, Name: "judge-mini"}
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "judge", dec.Reason)
}

func TestRouteFallbackOnBadJudge(t *testing.T) {
	e, fs, fj := newEngine()
	fs.judge = &store.Model{ID: 9, Name: "judge-mini"}
	fj.out = "nonexistent"
	fs.defGroup = puint(7)
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "fallback", dec.Reason)
}
