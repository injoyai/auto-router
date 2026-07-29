package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/store"
)

func TestHealthEndpoint(t *testing.T) {
	r := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"status":"ok"}`, w.Body.String())
}

func TestSessionCleanup(t *testing.T) {
	app := newTestApp(t, "http://example.com")
	// Pin the in-memory SQLite to a single connection so the cleanup goroutine
	// shares the same :memory: database (each :memory: connection otherwise
	// gets its own private, empty database). This is still required even with
	// the WAL/busy_timeout PRAGMAs from Open() — WAL does not apply to
	// :memory: databases, which remain per-connection.
	sqlDB, err := app.Store.DB.DB()
	assert.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// insert expired session directly
	if err := app.Store.SetNextModel("expired", "gpt-4o", -1*time.Hour); err != nil {
		t.Fatal(err)
	}
	// confirm the row exists physically before cleanup
	var n int64
	app.Store.DB.Model(&store.Session{}).Count(&n)
	assert.Equal(t, int64(1), n)

	StartSessionCleanup(app.Store, 50*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	// row should be physically deleted by the cleanup goroutine
	app.Store.DB.Model(&store.Session{}).Count(&n)
	assert.Equal(t, int64(0), n)
	_, err = app.Store.GetSession("expired")
	assert.Error(t, err) // expired -> not found
}
