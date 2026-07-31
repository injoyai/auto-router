package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
// non-2xx status yields ok=false with an "HTTP <status>: <body>" error that
// surfaces the upstream response body for diagnostics.
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

	// 401 -> ok=false, error includes status + upstream body
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
	assert.Contains(t, res["error"], "HTTP 401")
	assert.Contains(t, res["error"], "internal body must not leak")
}

// TestDeleteReferencedRejected (I10) verifies that deleting a provider with
// models returns 409 "in use", while model deletion always cascades (queue
// membership is a soft reference): the judge model, the former default model,
// and an unreferenced model can all be deleted.
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

	// Delete judge model (id=1) -> 200 (judge is now a queue member; soft
	// reference, deletion cascades the ModelGroupItem row).
	req = httptest.NewRequest(http.MethodDelete, "/admin/models/1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Delete model id=2 (former default model; default check removed, no longer blocks) -> 200
	req = httptest.NewRequest(http.MethodDelete, "/admin/models/2", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Create an unreferenced model + provider and delete them successfully.
	prov := &store.Provider{Name: "p2", BaseURL: "http://x", APIKey: store.Encrypt(app.CryptoKey, "k"), Protocol: "openai", Enabled: true}
	if err := app.Store.CreateProvider(prov); err != nil {
		t.Fatal(err)
	}
	m := &store.Model{Name: "free", ProviderID: prov.ID, Enabled: true}
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

func TestAdminGroupsCRUDAndItems(t *testing.T) {
	app := newTestApp(t, startMockUpstream(t))
	tok := adminToken(t, app)
	h := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf *bytes.Buffer
		if body != nil {
			b, _ := json.Marshal(body)
			buf = bytes.NewBuffer(b)
		} else {
			buf = bytes.NewBuffer(nil)
		}
		req := httptest.NewRequest(method, path, buf)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		app.Router.ServeHTTP(w, req)
		return w
	}

	// Create group "q". The seed group is id=1, so this is typically id=2;
	// parse the id from the response instead of hard-coding it.
	w := h("POST", "/admin/groups", groupInput{Name: "q", Enabled: true})
	assert.Equal(t, http.StatusOK, w.Code)
	var g store.ModelGroup
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &g))
	assert.NotZero(t, g.ID)
	qID := g.ID

	// List groups: response includes the newly created queue name "q".
	w = h("GET", "/admin/groups", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "q")

	// Update group and confirm the change via list.
	w = h("PUT", "/admin/groups/"+strconv.Itoa(int(qID)), groupInput{Name: "q-updated", Enabled: true})
	assert.Equal(t, http.StatusOK, w.Code)
	w = h("GET", "/admin/groups", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "q-updated")

	// Replace items on the seeded group (id=1) and verify ordering/content.
	ms, _ := app.Store.ListModels()
	assert.NotEmpty(t, ms)
	w = h("PUT", "/admin/groups/1/items", map[string]any{"items": []uint{ms[0].ID}})
	assert.Equal(t, http.StatusOK, w.Code)

	w = h("GET", "/admin/groups/1/items", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data")
	var itemsResp struct {
		Data []struct {
			ModelID uint `json:"model_id"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &itemsResp))
	assert.Len(t, itemsResp.Data, 1)
	assert.Contains(t, []uint{itemsResp.Data[0].ModelID}, ms[0].ID)

	// Delete a non-default group succeeds (q is not the default queue).
	w = h("DELETE", "/admin/groups/"+strconv.Itoa(int(qID)), nil)
	assert.Equal(t, http.StatusOK, w.Code)

	// Deleting the default group is rejected with 409.
	defID := uint(1)
	assert.NoError(t, app.Store.UpdateRoutingConfig(&store.RoutingConfig{ID: 1, DefaultGroupID: &defID, JudgeMaxInputChars: 1000}))
	w = h("DELETE", "/admin/groups/1", nil)
	assert.Equal(t, http.StatusConflict, w.Code)
}
