package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/routing"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

// startJudgeStub returns an httptest server that responds with the given
// status code and body. Used to stub the judge upstream.
func startJudgeStub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newLazyJudgeTestStore seeds an in-memory store with two providers (pointed at
// the given upstream URLs) and two judge models, returning the ordered chain.
// The first model is wired to failURL, the second to okURL.
func newLazyJudgeTestStore(t *testing.T, failURL, okURL string) (*store.Store, []byte, []*store.Model) {
	t.Helper()
	st, err := store.Open(store.SQLiteDialer{}, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	key := store.DeriveKey("test-seed")

	provFail := &store.Provider{Name: "fail-prov", BaseURL: failURL, APIKey: store.Encrypt(key, "sk-fail"), Protocol: "openai", Enabled: true}
	if err := st.CreateProvider(provFail); err != nil {
		t.Fatal(err)
	}
	provOK := &store.Provider{Name: "ok-prov", BaseURL: okURL, APIKey: store.Encrypt(key, "sk-ok"), Protocol: "openai", Enabled: true}
	if err := st.CreateProvider(provOK); err != nil {
		t.Fatal(err)
	}

	mFail := &store.Model{Name: "judge-fail", ProviderID: provFail.ID, Enabled: true}
	if err := st.CreateModel(mFail); err != nil {
		t.Fatal(err)
	}
	mOK := &store.Model{Name: "judge-ok", ProviderID: provOK.ID, Enabled: true}
	if err := st.CreateModel(mOK); err != nil {
		t.Fatal(err)
	}
	return st, key, []*store.Model{mFail, mOK}
}

const judgeOKBody = `{"model":"judge-ok","choices":[{"index":0,"message":{"role":"assistant","content":"deepseek-v4-flash"},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`

// TestLazyJudgeFailover verifies that lazyJudge.Judge walks the chain and
// returns the first successful response: the first model's upstream fails
// (HTTP 500), so the judge falls over to the second model and returns its raw
// output with servedModel set to the second model's name.
func TestLazyJudgeFailover(t *testing.T) {
	failSrv := startJudgeStub(t, http.StatusInternalServerError, `{"error":"down"}`)
	okSrv := startJudgeStub(t, http.StatusOK, judgeOKBody)

	st, key, chain := newLazyJudgeTestStore(t, failSrv.URL, okSrv.URL)
	lj := &lazyJudge{st: st, disp: upstream.New(), key: key}

	raw, servedModel, usage, _, err := lj.Judge(chain, []routing.Candidate{{Name: "deepseek-v4-flash"}}, "hi", nil)
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", raw)
	assert.Equal(t, "judge-ok", servedModel)
	assert.NotNil(t, usage)
	assert.EqualValues(t, 7, usage.TotalTokens)
}

// TestLazyJudgeAllFail verifies that when every model in the chain fails,
// lazyJudge.Judge returns an error containing "judge queue exhausted".
func TestLazyJudgeAllFail(t *testing.T) {
	failSrv1 := startJudgeStub(t, http.StatusInternalServerError, `{"error":"down"}`)
	failSrv2 := startJudgeStub(t, http.StatusInternalServerError, `{"error":"down"}`)

	st, key, chain := newLazyJudgeTestStore(t, failSrv1.URL, failSrv2.URL)
	lj := &lazyJudge{st: st, disp: upstream.New(), key: key}

	raw, servedModel, usage, _, err := lj.Judge(chain, []routing.Candidate{{Name: "deepseek-v4-flash"}}, "hi", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "judge queue exhausted")
	assert.Empty(t, raw)
	assert.Empty(t, servedModel)
	assert.Nil(t, usage)
}

// judgeEmptyBody is a valid 200 response whose choice content is empty.
const judgeEmptyBody = `{"model":"judge-fail","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"total_tokens":3}}`

// TestLazyJudgeEmptyContentFailover verifies that when a judge model returns
// HTTP 200 but empty content, lazyJudge treats it as a failed attempt: the
// trace attempt is marked failed with a descriptive error, and judging falls
// over to the next model in the chain.
func TestLazyJudgeEmptyContentFailover(t *testing.T) {
	emptySrv := startJudgeStub(t, http.StatusOK, judgeEmptyBody)
	okSrv := startJudgeStub(t, http.StatusOK, judgeOKBody)

	st, key, chain := newLazyJudgeTestStore(t, emptySrv.URL, okSrv.URL)
	lj := &lazyJudge{st: st, disp: upstream.New(), key: key}

	raw, servedModel, _, trace, err := lj.Judge(chain, []routing.Candidate{{Name: "deepseek-v4-flash"}}, "hi", nil)
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", raw)
	assert.Equal(t, "judge-ok", servedModel)

	// 第一个模型 HTTP 200 但内容为空,其最后一次 attempt 应标记为失败并说明原因
	var firstAttempts []store.Attempt
	for _, a := range trace {
		if a.Model == "judge-fail" {
			firstAttempts = append(firstAttempts, a)
		}
	}
	assert.NotEmpty(t, firstAttempts)
	last := firstAttempts[len(firstAttempts)-1]
	assert.False(t, last.Success, "empty-content attempt should be marked failed")
	assert.Contains(t, last.Error, "empty content")
}

// TestLazyJudgeAllEmpty verifies that when every judge model returns HTTP 200
// but empty content, lazyJudge returns an error that explains the cause
// (instead of a bare "judge queue exhausted" with no wrapped error).
func TestLazyJudgeAllEmpty(t *testing.T) {
	emptySrv1 := startJudgeStub(t, http.StatusOK, judgeEmptyBody)
	emptySrv2 := startJudgeStub(t, http.StatusOK, judgeEmptyBody)

	st, key, chain := newLazyJudgeTestStore(t, emptySrv1.URL, emptySrv2.URL)
	lj := &lazyJudge{st: st, disp: upstream.New(), key: key}

	_, _, _, _, err := lj.Judge(chain, []routing.Candidate{{Name: "deepseek-v4-flash"}}, "hi", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")
}

// judgeNoChoicesBody is a valid 200 response with an empty choices array.
const judgeNoChoicesBody = `{"model":"judge-fail","choices":[],"usage":{"total_tokens":3}}`

// TestLazyJudgeNoChoicesFailover verifies that when a judge model returns HTTP 200
// but an empty choices array, lazyJudge treats it as a failed attempt: the trace
// attempt is marked failed with a descriptive error, and judging falls over to
// the next model in the chain. This covers the case where defaultJudgeClient
// returns err="judge returned no choices" while the HTTP-level attempt was
// marked Success=true -- without correction the trace would misleadingly show
// "Success=true yet continued to the next model".
func TestLazyJudgeNoChoicesFailover(t *testing.T) {
	noChoicesSrv := startJudgeStub(t, http.StatusOK, judgeNoChoicesBody)
	okSrv := startJudgeStub(t, http.StatusOK, judgeOKBody)

	st, key, chain := newLazyJudgeTestStore(t, noChoicesSrv.URL, okSrv.URL)
	lj := &lazyJudge{st: st, disp: upstream.New(), key: key}

	raw, servedModel, _, trace, err := lj.Judge(chain, []routing.Candidate{{Name: "deepseek-v4-flash"}}, "hi", nil)
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", raw)
	assert.Equal(t, "judge-ok", servedModel)

	// 第一个模型 HTTP 200 但 choices 为空,其最后一次 attempt 应标记为失败并说明原因
	var firstAttempts []store.Attempt
	for _, a := range trace {
		if a.Model == "judge-fail" {
			firstAttempts = append(firstAttempts, a)
		}
	}
	assert.NotEmpty(t, firstAttempts)
	last := firstAttempts[len(firstAttempts)-1]
	assert.False(t, last.Success, "no-choices attempt should be marked failed")
	assert.NotEmpty(t, last.Error, "no-choices attempt should carry an error message")
}
