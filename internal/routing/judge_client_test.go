package routing

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

func TestJudgeClientCallsUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"judge-mini","choices":[{"index":0,"message":{"role":"assistant","content":"gpt-4o"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
	}))
	defer srv.Close()

	jc := NewJudgeClient(upstream.New(), srv.URL, "sk-judge", "openai")
	judgeModel := &store.Model{Name: "judge-mini"}
	out, _, err := jc.Judge(judgeModel, []store.Model{{Name: "gpt-4o"}}, "hi")
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", out)
}
