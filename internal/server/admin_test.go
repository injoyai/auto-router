package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/store"
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

// TestDeleteReferencedRejected (I10) verifies that deleting a provider with
// models, the judge model, and the default model all return 409 "in use",
// while an unreferenced model/provider can be deleted.
func TestDeleteReferencedRejected(t *testing.T) {
	app := newTestApp(t, "http://example.com")
	tok := adminToken(t, app)

	// newTestApp created provider 1 with judge model (id=1) and target model
	// (id=2, the default). Both are referenced; provider 1 has models.
	// Delete provider 1 -> 409 (has models)
	req := httptest.NewRequest(http.MethodDelete, "/admin/providers/1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "in use")

	// Delete judge model (id=1) -> 409 (is_judge)
	req = httptest.NewRequest(http.MethodDelete, "/admin/models/1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Delete default model (id=2) -> 409 (default_model_id)
	req = httptest.NewRequest(http.MethodDelete, "/admin/models/2", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Create an unreferenced model + provider and delete them successfully.
	prov := &store.Provider{Name: "p2", BaseURL: "http://x", APIKey: store.Encrypt(app.CryptoKey, "k"), Protocol: "openai", Enabled: true}
	if err := app.Store.CreateProvider(prov); err != nil {
		t.Fatal(err)
	}
	m := &store.Model{Name: "free", DisplayName: "Free", ProviderID: prov.ID, Enabled: true}
	if err := app.Store.CreateModel(m); err != nil {
		t.Fatal(err)
	}
	// Delete the unreferenced model -> 200
	req = httptest.NewRequest(http.MethodDelete, "/admin/models/3", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	// Now the provider has no models -> delete -> 200
	req = httptest.NewRequest(http.MethodDelete, "/admin/providers/2", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
