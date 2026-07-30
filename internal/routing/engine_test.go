package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

type fakeStore struct {
	judge  *store.Model
	def    *store.Model
	byName map[string]*store.Model
}

func (f *fakeStore) GetJudgeModel() (*store.Model, error)            { return f.judge, nil }
func (f *fakeStore) GetRoutingConfig() (*store.RoutingConfig, error) { return &store.RoutingConfig{DefaultModelID: puint(2), JudgeMaxInputChars: 1000}, nil }
func (f *fakeStore) GetModel(id uint) (*store.Model, error)          { return f.byName["m"+itoa(id)], nil }
func (f *fakeStore) GetModelByName(n string) (*store.Model, error)   { return f.byName[n], nil }
func (f *fakeStore) ListEnabledModels() ([]store.Model, error) {
	var out []store.Model
	for _, m := range f.byName {
		out = append(out, *m)
	}
	return out, nil
}

type fakeJudge struct {
	out string
	err error
}

func (fj *fakeJudge) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, *model.Usage, error) {
	return fj.out, nil, fj.err
}

func puint(v uint) *uint { return &v }
func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	digits := []byte{}
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

func newEngine() (*Engine, *fakeStore, *fakeJudge) {
	fs := &fakeStore{
		byName: map[string]*store.Model{
			"gpt-4o": {ID: 1, Name: "gpt-4o", Enabled: true},
			"m2":     {ID: 2, Name: "default-model", Enabled: true},
		},
	}
	fj := &fakeJudge{out: "gpt-4o"}
	return New(fs, fj), fs, fj
}

func TestRouteOverride(t *testing.T) {
	e, _, _ := newEngine()
	req := &model.ChatRequest{Override: "gpt-4o"}
	dec, err := e.Route(req)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", dec.ModelName)
	assert.Equal(t, "override", dec.Reason)
}

func TestRouteJudge(t *testing.T) {
	e, fs, _ := newEngine()
	fs.judge = &store.Model{ID: 9, Name: "judge-mini"}
	req := &model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}}
	dec, err := e.Route(req)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", dec.ModelName)
	assert.Equal(t, "judge", dec.Reason)
	assert.Equal(t, "gpt-4o", dec.JudgeRaw)
}

func TestRouteFallbackOnBadJudge(t *testing.T) {
	e, fs, fj := newEngine()
	fs.judge = &store.Model{ID: 9, Name: "judge-mini"}
	fj.out = "nonexistent"
	req := &model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}}
	dec, err := e.Route(req)
	assert.NoError(t, err)
	assert.Equal(t, "default-model", dec.ModelName)
	assert.Equal(t, "fallback", dec.Reason)
}

func TestRouteFallbackNoJudge(t *testing.T) {
	e, fs, _ := newEngine()
	fs.judge = nil
	req := &model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}}
	dec, err := e.Route(req)
	assert.NoError(t, err)
	assert.Equal(t, "fallback", dec.Reason)
}
