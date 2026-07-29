package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdminLoginAndProviders(t *testing.T) {
	app := newTestApp(t, "http://example.com")

	// login
	body, _ := json.Marshal(map[string]string{"token": "admin-token"})
	req := httptest.NewRequest(http.MethodPost, "/admin/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var lr struct{ Token string }
	_ = json.Unmarshal(w.Body.Bytes(), &lr)
	assert.NotEmpty(t, lr.Token)

	// list providers with token
	req = httptest.NewRequest(http.MethodGet, "/admin/providers", nil)
	req.Header.Set("Authorization", "Bearer "+lr.Token)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAdminLoginBadToken(t *testing.T) {
	app := newTestApp(t, "http://example.com")
	body, _ := json.Marshal(map[string]string{"token": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/admin/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// startTestConnectUpstream serves GET /models with the given status code.
func startTestConnectUpstream(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, "internal body must not leak")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func adminToken(t *testing.T, app *testApp) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": "admin-token"})
	req := httptest.NewRequest(http.MethodPost, "/admin/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	var lr struct{ Token string }
	_ = json.Unmarshal(w.Body.Bytes(), &lr)
	return lr.Token
}

// TestTestProviderOkLogic (I6) verifies ok is true only for 2xx and that a
// non-2xx status yields ok=false with a generic "HTTP <status>" error (and the
// upstream body is not leaked to the client).
func TestTestProviderOkLogic(t *testing.T) {
	// 2xx -> ok=true
	app := newTestApp(t, startTestConnectUpstream(t, http.StatusOK))
	tok := adminToken(t, app)
	req := httptest.NewRequest(http.MethodPost, "/admin/providers/1/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	assert.Equal(t, true, res["ok"])
	assert.EqualValues(t, http.StatusOK, res["status"])

	// 401 -> ok=false, generic error, body not leaked
	app2 := newTestApp(t, startTestConnectUpstream(t, http.StatusUnauthorized))
	tok2 := adminToken(t, app2)
	req = httptest.NewRequest(http.MethodPost, "/admin/providers/1/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok2)
	w = httptest.NewRecorder()
	app2.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	assert.Equal(t, false, res["ok"])
	assert.EqualValues(t, http.StatusUnauthorized, res["status"])
	assert.Equal(t, "HTTP 401", res["error"])
	assert.NotContains(t, w.Body.String(), "internal body must not leak")
}
