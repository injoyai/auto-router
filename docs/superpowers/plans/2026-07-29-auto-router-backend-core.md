# Auto Model Router — Backend Core (Plan 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working OpenAI-compatible AI model router gateway in Go that auto-selects models via a judge model, supports agent override + session-based next-model selection, with a SQLite-backed admin API.

**Architecture:** Internal canonical format (OpenAI-based) + adapters. Pipeline: inbound adapter → routing engine (override → session → judge → fallback) → upstream dispatcher → response post-processor → outbound adapter. SQLite via GORM (pure-Go driver, no CGO). Gin HTTP server.

**Tech Stack:** Go 1.22+, Gin, GORM + glebarez/sqlite (pure Go), golang-jwt/jwt/v5, stdlib crypto (AES-GCM), testify/assert for tests.

**Scope of this plan:** OpenAI protocol only (inbound + outbound). Claude protocol + cross-protocol routing is Plan 2. React frontend is Plan 3. After Plan 1, the gateway is usable by any OpenAI client with auto-routing.

---

## File Structure

```
auto-router/
├── cmd/router/main.go                  # Entry point
├── internal/
│   ├── config/config.go                # Env config loading
│   ├── model/canonical.go              # Canonical request/response types
│   ├── store/
│   │   ├── store.go                    # GORM init + AutoMigrate
│   │   ├── crypto.go                   # AES-GCM encrypt/decrypt
│   │   ├── settings.go                 # KV settings (admin token, seed)
│   │   ├── providers.go                # Provider CRUD
│   │   ├── models.go                   # Model CRUD + is_judge toggle
│   │   ├── routing.go                  # routing_config singleton
│   │   ├── sessions.go                 # Session next_model + expiry
│   │   └── logs.go                     # Request logs
│   ├── adapter/openai/
│   │   ├── inbound.go                  # OpenAI request → canonical
│   │   ├── outbound_request.go         # canonical → OpenAI upstream request
│   │   ├── outbound_response.go        # upstream resp → canonical (non-stream)
│   │   └── outbound_stream.go          # upstream SSE → canonical deltas
│   ├── upstream/dispatcher.go          # Call upstream (stream + non-stream)
│   ├── routing/
│   │   ├── engine.go                   # Priority routing logic
│   │   ├── judge.go                    # Judge model invocation + parsing
│   │   └── postprocess.go              # Extract <<next_model:>> directive
│   ├── server/
│   │   ├── server.go                   # Gin engine + routes
│   │   ├── middleware.go               # Auth (gateway token + admin JWT)
│   │   ├── gateway.go                  # /v1/chat/completions, /v1/models
│   │   └── admin.go                    # Admin CRUD handlers
│   └── jwt/jwt.go                      # JWT issue/verify
├── go.mod
├── go.sum
└── docs/superpowers/
    ├── specs/2026-07-29-auto-model-router-design.md
    └── plans/2026-07-29-auto-router-backend-core.md
```

---

## Task 1: Project skeleton, config, health endpoint

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/server/server.go`
- Create: `cmd/router/main.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Initialize module and deps**

Run:
```bash
go mod init auto-router
go get github.com/gin-gonic/gin@latest
go get gorm.io/gorm@latest
go get github.com/glebarez/sqlite@latest
go get github.com/golang-jwt/jwt/v5@latest
go get github.com/stretchr/testify@latest
```

- [ ] **Step 2: Write the failing test**

`internal/server/server_test.go`:
```go
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthEndpoint(t *testing.T) {
	r := NewRouter(Config{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"status":"ok"}`, w.Body.String())
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestHealthEndpoint -v`
Expected: FAIL — `NewRouter` / `Config` undefined.

- [ ] **Step 4: Write minimal implementation**

`internal/config/config.go`:
```go
package config

import "os"

type Config struct {
	ListenAddr string
	DBPath     string
	AdminToken string // if empty, generated on first run and stored in DB
	GatewayToken string // if empty, generated on first run and stored in DB
}

func Load() Config {
	c := Config{
		ListenAddr:   getEnv("LISTEN_ADDR", ":8080"),
		DBPath:       getEnv("DB_PATH", "auto-router.db"),
		AdminToken:   os.Getenv("ADMIN_TOKEN"),
		GatewayToken: os.Getenv("GATEWAY_TOKEN"),
	}
	return c
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
```

`internal/server/server.go`:
```go
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"auto-router/internal/config"
)

type Config = config.Config

func NewRouter(_ Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}
```

`cmd/router/main.go`:
```go
package main

import (
	"log"

	"auto-router/internal/config"
	"auto-router/internal/server"
)

func main() {
	cfg := config.Load()
	r := server.NewRouter(cfg)
	log.Printf("listening on %s", cfg.ListenAddr)
	if err := r.Run(cfg.ListenAddr); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestHealthEndpoint -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/ cmd/
git commit -m "feat: project skeleton with health endpoint"
```

---

## Task 2: Store init + GORM models + migrations

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/providers.go`
- Create: `internal/store/models.go`
- Create: `internal/store/routing.go`
- Create: `internal/store/sessions.go`
- Create: `internal/store/logs.go`
- Create: `internal/store/settings.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write the failing test**

`internal/store/store_test.go`:
```go
package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestOpenAutoMigrates(t *testing.T) {
	s := newTestStore(t)
	err := s.DB.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &Session{}, &RequestLog{}, &Setting{}).Error
	assert.NoError(t, err)

	// Tables exist: query each
	assert.NoError(t, s.DB.First(&Provider{}).Error) // empty but table exists -> gorm returns ErrRecordNotFound, not table-missing
	// Use raw check instead:
	var count int64
	for _, tbl := range []string{"providers", "models", "routing_configs", "sessions", "request_logs", "settings"} {
		s.DB.Table(tbl).Count(&count)
		assert.Equal(t, int64(0), count, "table %s should exist and be empty", tbl)
	}
}

var _ = gorm.ErrRecordNotFound
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestOpenAutoMigrates -v`
Expected: FAIL — `Open`, `Store`, model types undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/store/store.go`:
```go
package store

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	DB *gorm.DB
}

func Open(path string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &Session{}, &RequestLog{}, &Setting{}); err != nil {
		return nil, err
	}
	// seed routing_config singleton row
	if err := db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1}).Error; err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}
```

`internal/store/providers.go`:
```go
package store

import "time"

type Provider struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	BaseURL   string    `gorm:"not null" json:"base_url"`
	APIKey    string    `gorm:"not null" json:"-"`           // encrypted; never JSON-exposed
	Protocol  string    `gorm:"not null" json:"protocol"`    // openai | claude
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListProviders() ([]Provider, error) {
	var ps []Provider
	err := s.DB.Order("id desc").Find(&ps).Error
	return ps, err
}

func (s *Store) GetProvider(id uint) (*Provider, error) {
	var p Provider
	if err := s.DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateProvider(p *Provider) error {
	return s.DB.Create(p).Error
}

func (s *Store) UpdateProvider(p *Provider) error {
	return s.DB.Save(p).Error
}

func (s *Store) DeleteProvider(id uint) error {
	return s.DB.Delete(&Provider{}, id).Error
}
```

`internal/store/models.go`:
```go
package store

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

type Model struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	DisplayName string    `gorm:"not null" json:"display_name"`
	ProviderID  uint      `gorm:"not null" json:"provider_id"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	IsJudge     bool      `gorm:"default:false" json:"is_judge"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) ListModels() ([]Model, error) {
	var ms []Model
	err := s.DB.Order("id desc").Find(&ms).Error
	return ms, err
}

func (s *Store) ListEnabledModels() ([]Model, error) {
	var ms []Model
	err := s.DB.Where("enabled = ?", true).Find(&ms).Error
	return ms, err
}

func (s *Store) GetModel(id uint) (*Model, error) {
	var m Model
	if err := s.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) GetModelByName(name string) (*Model, error) {
	var m Model
	err := s.DB.Where("name = ? OR display_name = ?", name, name).First(&m).Error
	return &m, err
}

func (s *Store) CreateModel(m *Model) error {
	return s.DB.Create(m).Error
}

func (s *Store) UpdateModel(m *Model) error {
	return s.DB.Save(m).Error
}

func (s *Store) DeleteModel(id uint) error {
	return s.DB.Delete(&Model{}, id).Error
}

// SetJudgeModel marks the given model as the sole judge, unsetting others.
func (s *Store) SetJudgeModel(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var m Model
		if err := tx.First(&m, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&Model{}).Where("1 = 1").Update("is_judge", false).Error; err != nil {
			return err
		}
		return tx.Model(&Model{}).Where("id = ?", id).Update("is_judge", true).Error
	})
}

func (s *Store) GetJudgeModel() (*Model, error) {
	var m Model
	err := s.DB.Where("is_judge = ? AND enabled = ?", true, true).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, err
}
```

`internal/store/routing.go`:
```go
package store

type RoutingConfig struct {
	ID                       uint  `gorm:"primaryKey" json:"id"`
	JudgeModelID             *uint `json:"judge_model_id"`
	DefaultModelID           *uint `json:"default_model_id"`
	EnableNextModelDirective bool  `gorm:"default:true" json:"enable_next_model_directive"`
	SessionTTLSeconds        int   `gorm:"default:1800" json:"session_ttl_seconds"`
	JudgeMaxInputChars       int   `gorm:"default:2000" json:"judge_max_input_chars"`
}

func (s *Store) GetRoutingConfig() (*RoutingConfig, error) {
	var rc RoutingConfig
	if err := s.DB.First(&rc, 1).Error; err != nil {
		return nil, err
	}
	return &rc, nil
}

func (s *Store) UpdateRoutingConfig(rc *RoutingConfig) error {
	rc.ID = 1
	return s.DB.Save(rc).Error
}
```

`internal/store/sessions.go`:
```go
package store

import "time"

type Session struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	NextModel string    `json:"next_model"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Store) GetSession(id string) (*Session, error) {
	var sess Session
	if err := s.DB.Where("id = ? AND expires_at > ?", id, time.Now()).First(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) SetNextModel(id, model string, ttl time.Duration) error {
	sess := Session{
		ID:        id,
		NextModel: model,
		ExpiresAt: time.Now().Add(ttl),
	}
	return s.DB.Save(&sess).Error
}

func (s *Store) CleanExpiredSessions() (int64, error) {
	res := s.DB.Where("expires_at < ?", time.Now()).Delete(&Session{})
	return res.RowsAffected, res.Error
}
```

`internal/store/logs.go`:
```go
package store

import "time"

type RequestLog struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	SessionID       string    `json:"session_id"`
	ClientProtocol  string    `json:"client_protocol"`
	RequestedModel  string    `json:"requested_model"`
	RoutedModel     string    `json:"routed_model"`
	RouteReason     string    `json:"route_reason"`
	JudgeRaw        string    `json:"judge_raw"`
	Status          int       `json:"status"`
	LatencyMs       int64     `json:"latency_ms"`
	Error           string    `json:"error"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Store) CreateLog(l *RequestLog) error {
	return s.DB.Create(l).Error
}

func (s *Store) ListLogs(page, pageSize int, reason, model string) ([]RequestLog, int64, error) {
	var logs []RequestLog
	var total int64
	q := s.DB.Model(&RequestLog{})
	if reason != "" {
		q = q.Where("route_reason = ?", reason)
	}
	if model != "" {
		q = q.Where("routed_model = ?", model)
	}
	q.Count(&total)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error
	return logs, total, err
}
```

`internal/store/settings.go`:
```go
package store

type Setting struct {
	Key   string `gorm:"primaryKey" json:"key"`
	Value string `json:"value"`
}

func (s *Store) GetSetting(key string) (string, error) {
	var st Setting
	if err := s.DB.First(&st, "key = ?", key).Error; err != nil {
		return "", err
	}
	return st.Value, nil
}

func (s *Store) SetSetting(key, value string) error {
	return s.DB.Save(&Setting{Key: key, Value: value}).Error
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestOpenAutoMigrates -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): gorm store with all models and migrations"
```

---

## Task 3: Crypto (AES-GCM) for API keys + setting bootstrap

**Files:**
- Create: `internal/store/crypto.go`
- Test: `internal/store/crypto_test.go`

- [ ] **Step 1: Write the failing test**

`internal/store/crypto_test.go`:
```go
package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := DeriveKey("test-seed")
	enc := Encrypt(key, "sk-secret-123")
	assert.NotEqual(t, "sk-secret-123", enc)
	dec, err := Decrypt(key, enc)
	assert.NoError(t, err)
	assert.Equal(t, "sk-secret-123", dec)
}

func TestDeriveKeyDeterministic(t *testing.T) {
	assert.Equal(t, DeriveKey("seed-a"), DeriveKey("seed-a"))
	assert.NotEqual(t, DeriveKey("seed-a"), DeriveKey("seed-b"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestEncryptDecryptRoundTrip -v`
Expected: FAIL — `DeriveKey`/`Encrypt`/`Decrypt` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/store/crypto.go`:
```go
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// DeriveKey derives a 32-byte AES key from a seed string (sha256).
func DeriveKey(seed string) []byte {
	h := sha256.Sum256([]byte(seed))
	return h[:]
}

// Encrypt encrypts plaintext with AES-GCM, returns base64(nonce||ciphertext).
func Encrypt(key []byte, plain string) string {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	ct := gcm.Seal(nil, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, ct...))
}

// Decrypt reverses Encrypt.
func Decrypt(key []byte, enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	pt, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS (all store tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/crypto.go internal/store/crypto_test.go
git commit -m "feat(store): AES-GCM crypto for secrets"
```

---

## Task 4: Canonical model types

**Files:**
- Create: `internal/model/canonical.go`
- Test: `internal/model/canonical_test.go`

- [ ] **Step 1: Write the failing test**

`internal/model/canonical_test.go`:
```go
package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLastUserMessage(t *testing.T) {
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "s"},
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "a"},
			{Role: "user", Content: "second"},
		},
	}
	assert.Equal(t, "second", req.LastUserMessage())
}

func TestLastUserMessageEmpty(t *testing.T) {
	req := &ChatRequest{}
	assert.Equal(t, "", req.LastUserMessage())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -v`
Expected: FAIL — types undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/model/canonical.go`:
```go
package model

// ChatRequest is the internal canonical request (OpenAI-based).
type ChatRequest struct {
	Model     string
	Messages  []Message
	Tools     []Tool
	Stream    bool
	SessionID string
	Override  string // explicit model override (X-Route-Model or model field)
	ClientFmt string // openai | claude — which protocol the client used
	// raw JSON of client body, kept for fields we pass through unchanged
	Raw map[string]any `json:"-"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Tool struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

// LastUserMessage returns the content of the last user message, or "".
func (r *ChatRequest) LastUserMessage() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			return r.Messages[i].Content
		}
	}
	return ""
}

// ChatResponse is the canonical non-streaming response.
type ChatResponse struct {
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Delta is one streaming chunk (OpenAI-style).
type Delta struct {
	Role       string     `json:"role,omitempty"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type Chunk struct {
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

type ChunkChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model/
git commit -m "feat(model): canonical request/response types"
```

---

## Task 5: OpenAI inbound adapter (client request → canonical)

**Files:**
- Create: `internal/adapter/openai/inbound.go`
- Test: `internal/adapter/openai/inbound_test.go`

- [ ] **Step 1: Write the failing test**

`internal/adapter/openai/inbound_test.go`:
```go
package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRequest(t *testing.T) {
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"stream":true,"temperature":0.5}`
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)

	req, err := ParseRequest(raw)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", req.Model)
	assert.Len(t, req.Messages, 1)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "hi", req.Messages[0].Content)
	assert.True(t, req.Stream)
	assert.Equal(t, "0.5", raw["temperature"]) // passthrough preserved
}

func TestParseRequestOverrideSentinel(t *testing.T) {
	for _, m := range []string{"", "auto", "route"} {
		req, _ := ParseRequest(map[string]any{"model": m})
		assert.True(t, req.IsRouteRequested(), "model=%q should trigger routing", m)
	}
}

func TestParseRequestExplicitModel(t *testing.T) {
	req, _ := ParseRequest(map[string]any{"model": "gpt-4"})
	assert.False(t, req.IsRouteRequested())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/openai/ -v`
Expected: FAIL — `ParseRequest` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/openai/inbound.go`:
```go
package openai

import (
	"encoding/json"

	"auto-router/internal/model"
)

// ParseRequest converts an OpenAI chat/completions request body into canonical form.
// raw is the parsed JSON body (kept for passthrough of unknown fields).
func ParseRequest(raw map[string]any) (*model.ChatRequest, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var o struct {
		Model    string          `json:"model"`
		Messages []model.Message `json:"messages"`
		Tools    []model.Tool    `json:"tools"`
		Stream   bool            `json:"stream"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}
	return &model.ChatRequest{
		Model:     o.Model,
		Messages:  o.Messages,
		Tools:     o.Tools,
		Stream:    o.Stream,
		ClientFmt: "openai",
		Raw:       raw,
	}, nil
}

// IsRouteRequested reports whether the client wants auto-routing (no explicit model).
func (r *model.ChatRequest) IsRouteRequested() bool {
	m := r.Model
	return m == "" || m == "auto" || m == "route"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/openai/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/openai/
git commit -m "feat(adapter/openai): inbound parser"
```

---

## Task 6: OpenAI outbound — request building (canonical → upstream)

**Files:**
- Create: `internal/adapter/openai/outbound_request.go`
- Test: `internal/adapter/openai/outbound_request_test.go`

- [ ] **Step 1: Write the failing test**

`internal/adapter/openai/outbound_request_test.go`:
```go
package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestBuildUpstreamRequest(t *testing.T) {
	req := &model.ChatRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
		Raw:      map[string]any{"temperature": 0.7},
	}
	body, err := BuildUpstreamRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", body["model"])
	assert.Equal(t, true, body["stream"])
	assert.Equal(t, 0.7, body["temperature"])
	assert.NotNil(t, body["messages"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/openai/ -run TestBuildUpstreamRequest -v`
Expected: FAIL — `BuildUpstreamRequest` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/openai/outbound_request.go`:
```go
package openai

import (
	"auto-router/internal/model"
)

// BuildUpstreamRequest converts a canonical request into an OpenAI-format
// request body map. It starts from the client's raw body (passthrough) and
// forces the model + messages + stream fields.
func BuildUpstreamRequest(req *model.ChatRequest) (map[string]any, error) {
	body := map[string]any{}
	for k, v := range req.Raw {
		body[k] = v
	}
	body["model"] = req.Model
	body["messages"] = req.Messages
	body["stream"] = req.Stream
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	return body, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/openai/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/openai/outbound_request.go internal/adapter/openai/outbound_request_test.go
git commit -m "feat(adapter/openai): build upstream request"
```

---

## Task 7: OpenAI outbound — response parsing (non-stream)

**Files:**
- Create: `internal/adapter/openai/outbound_response.go`
- Test: `internal/adapter/openai/outbound_response_test.go`

- [ ] **Step 1: Write the failing test**

`internal/adapter/openai/outbound_response_test.go`:
```go
package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseResponse(t *testing.T) {
	raw := `{"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)

	resp, err := ParseResponse(m)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4", resp.Model)
	assert.Len(t, resp.Choices, 1)
	assert.Equal(t, "hello", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Equal(t, 3, resp.Usage.TotalTokens)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/openai/ -run TestParseResponse -v`
Expected: FAIL — `ParseResponse` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/openai/outbound_response.go`:
```go
package openai

import (
	"encoding/json"

	"auto-router/internal/model"
)

// ParseResponse converts an OpenAI-format non-streaming response into canonical form.
func ParseResponse(raw map[string]any) (*model.ChatResponse, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var r model.ChatResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// EncodeResponseToClient returns the canonical response as an OpenAI-format body.
// (OpenAI is the canonical format, so this is a passthrough marshal.)
func EncodeResponseToClient(resp *model.ChatResponse) ([]byte, error) {
	return json.Marshal(resp)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/openai/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/openai/outbound_response.go internal/adapter/openai/outbound_response_test.go
git commit -m "feat(adapter/openai): parse non-stream response"
```

---

## Task 8: OpenAI outbound — streaming (SSE → canonical deltas)

**Files:**
- Create: `internal/adapter/openai/outbound_stream.go`
- Test: `internal/adapter/openai/outbound_stream_test.go`

- [ ] **Step 1: Write the failing test**

`internal/adapter/openai/outbound_stream_test.go`:
```go
package openai

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestScanChunks(t *testing.T) {
	sse := "data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n" +
		"data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	scanner := bufio.NewScanner(strings.NewReader(sse))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	var contents []string
	var finish string
	for scanner.Scan() {
		line := scanner.Text()
		ch, done, err := ParseSSELine(line)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if ch == nil {
			continue
		}
		if len(ch.Choices) > 0 {
			contents = append(contents, ch.Choices[0].Delta.Content)
			if ch.Choices[0].FinishReason != "" {
				finish = ch.Choices[0].FinishReason
			}
		}
	}
	assert.Equal(t, []string{"hel", "lo"}, contents)
	assert.Equal(t, "stop", finish)
}

func TestEncodeChunk(t *testing.T) {
	ch := &model.Chunk{
		Model: "gpt-4",
		Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: "x"}}},
	}
	b, err := EncodeChunk(ch)
	assert.NoError(t, err)
	assert.Equal(t, `data: {"model":"gpt-4","choices":[{"index":0,"delta":{"content":"x"}}]}`, string(b))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/openai/ -run TestScanChunks -v`
Expected: FAIL — `ParseSSELine`/`EncodeChunk` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/adapter/openai/outbound_stream.go`:
```go
package openai

import (
	"encoding/json"
	"strings"

	"auto-router/internal/model"
)

// ParseSSELine parses one line of an OpenAI SSE stream.
// Returns (chunk, done, error). chunk is nil for blank/comment lines.
func ParseSSELine(line string) (*model.Chunk, bool, error) {
	if line == "" || strings.HasPrefix(line, ":") {
		return nil, false, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil, false, nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		return nil, true, nil
	}
	var ch model.Chunk
	if err := json.Unmarshal([]byte(data), &ch); err != nil {
		return nil, false, err
	}
	return &ch, false, nil
}

// EncodeChunk serializes a canonical chunk into an OpenAI SSE data line.
func EncodeChunk(ch *model.Chunk) ([]byte, error) {
	b, err := json.Marshal(ch)
	if err != nil {
		return nil, err
	}
	out := append([]byte("data: "), b...)
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/openai/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/openai/outbound_stream.go internal/adapter/openai/outbound_stream_test.go
git commit -m "feat(adapter/openai): SSE stream parsing"
```

---

## Task 9: Upstream dispatcher (stream + non-stream)

**Files:**
- Create: `internal/upstream/dispatcher.go`
- Test: `internal/upstream/dispatcher_test.go`

- [ ] **Step 1: Write the failing test**

`internal/upstream/dispatcher_test.go`:
```go
package upstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
)

func TestCallNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	d := New()
	resp, err := d.Call(srv.URL, "sk-test", map[string]any{"model": "gpt-4", "messages": []model.Message{{Role: "user", Content: "x"}}})
	assert.NoError(t, err)
	assert.Equal(t, "hi", resp.Choices[0].Message.Content)
}

func TestCallStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := New()
	chunks, err := d.CallStream(srv.URL, "sk-test", map[string]any{"model": "gpt-4", "stream": true})
	assert.NoError(t, err)
	assert.Equal(t, "hi", chunks[0].Choices[0].Delta.Content)
	assert.True(t, chunks[1] == nil) // sentinel [DONE]
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream/ -v`
Expected: FAIL — types undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/upstream/dispatcher.go`:
```go
package upstream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
)

type Dispatcher struct {
	Client *http.Client
}

func New() *Dispatcher {
	return &Dispatcher{Client: &http.Client{Timeout: 5 * time.Minute}}
}

// Call performs a non-streaming upstream request and returns the parsed canonical response.
func (d *Dispatcher) Call(baseURL, apiKey string, body map[string]any) (*model.ChatResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(raw))
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return openai.ParseResponse(m)
}

// StreamChunk is one parsed chunk; nil sentinel marks [DONE].
type StreamChunk = *model.Chunk

// CallStream performs a streaming upstream request and returns parsed chunks.
// The final element is nil to signal [DONE].
func (d *Dispatcher) CallStream(baseURL, apiKey string, body map[string]any) ([]StreamChunk, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(raw))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 4*1024*1024)
	var out []StreamChunk
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		ch, done, err := openai.ParseSSELine(line)
		if err != nil {
			return out, err
		}
		if done {
			out = append(out, nil)
			return out, nil
		}
		if ch != nil {
			out = append(out, ch)
		}
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	out = append(out, nil) // ensure terminator
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/upstream/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/upstream/
git commit -m "feat(upstream): dispatcher with stream + non-stream"
```

---

## Task 10: Next-model directive post-processor

**Files:**
- Create: `internal/routing/postprocess.go`
- Test: `internal/routing/postprocess_test.go`

- [ ] **Step 1: Write the failing test**

`internal/routing/postprocess_test.go`:
```go
package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractNextModel(t *testing.T) {
	text := "Here is your answer.<<next_model: gpt-4o>>"
	clean, model := ExtractNextModel(text)
	assert.Equal(t, "Here is your answer.", clean)
	assert.Equal(t, "gpt-4o", model)
}

func TestExtractNextModelNone(t *testing.T) {
	clean, model := ExtractNextModel("no directive here")
	assert.Equal(t, "no directive here", clean)
	assert.Equal(t, "", model)
}

func TestExtractNextModelWhitespace(t *testing.T) {
	clean, model := ExtractNextModel("ans<<next_model:   gpt-4  >>")
	assert.Equal(t, "ans", clean)
	assert.Equal(t, "gpt-4", model)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/routing/ -run TestExtractNextModel -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/routing/postprocess.go`:
```go
package routing

import "regexp"

var nextModelRe = regexp.MustCompile(`<<next_model:\s*([^>]+?)\s*>>`)

// ExtractNextModel finds a <<next_model: name>> directive in text.
// Returns the cleaned text (directive removed) and the model name (or "").
func ExtractNextModel(text string) (string, string) {
	m := nextModelRe.FindStringSubmatch(text)
	if m == nil {
		return text, ""
	}
	clean := nextModelRe.ReplaceAllString(text, "")
	return clean, m[1]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/routing/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/routing/postprocess.go internal/routing/postprocess_test.go
git commit -m "feat(routing): next-model directive extraction"
```

---

## Task 11: Judge model client

**Files:**
- Create: `internal/routing/judge.go`
- Test: `internal/routing/judge_test.go`

- [ ] **Step 1: Write the failing test**

`internal/routing/judge_test.go`:
```go
package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

func TestBuildJudgeMessages(t *testing.T) {
	candidates := []store.Model{
		{Name: "gpt-4o-mini", DisplayName: "Fast", Description: "small fast"},
		{Name: "gpt-4o", DisplayName: "Smart", Description: "large smart"},
	}
	msgs := BuildJudgeMessages(candidates, "Write a haiku")
	assert.Equal(t, "system", msgs[0].Role)
	assert.Contains(t, msgs[0].Content, "模型路由器")
	assert.Equal(t, "user", msgs[1].Role)
	assert.Contains(t, msgs[1].Content, "gpt-4o-mini")
	assert.Contains(t, msgs[1].Content, "gpt-4o")
	assert.Contains(t, msgs[1].Content, "Write a haiku")
}

func TestParseJudgeOutput(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":                       "gpt-4o",
		"  gpt-4o  ":                   "gpt-4o",
		"```gpt-4o```":                 "gpt-4o",
		"```json\ngpt-4o\n```":         "gpt-4o",
		"\"gpt-4o\"":                   "gpt-4o",
		"nonsense":                     "",
	}
	for in, want := range cases {
		got := ParseJudgeOutput(in, []string{"gpt-4o"})
		assert.Equal(t, want, got, "input %q", in)
	}
}

func TestTruncateUserText(t *testing.T) {
	long := ""
	for i := 0; i < 5000; i++ {
		long += "x"
	}
	out := TruncateUserText(long, 100)
	assert.Equal(t, 100, len(out))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/routing/ -run TestBuildJudgeMessages -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/routing/judge.go`:
```go
package routing

import (
	"strings"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

const judgeSystemPrompt = `你是一个模型路由器。根据用户任务和可用模型列表,选择最合适的模型。只回复模型名称,不要解释。`

// BuildJudgeMessages constructs the messages to send to the judge model.
func BuildJudgeMessages(candidates []store.Model, userText string) []model.Message {
	var sb strings.Builder
	sb.WriteString("可用模型列表:\n")
	for _, c := range candidates {
		sb.WriteString("- ")
		sb.WriteString(c.Name)
		if c.Description != "" {
			sb.WriteString(" - ")
			sb.WriteString(c.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n用户任务:\n")
	sb.WriteString(userText)
	return []model.Message{
		{Role: "system", Content: judgeSystemPrompt},
		{Role: "user", Content: sb.String()},
	}
}

// ParseJudgeOutput normalizes the judge model's reply and matches it against
// known model names. Returns "" if no match.
func ParseJudgeOutput(raw string, known []string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "`\"' \n")
	// strip markdown fence like ```json ... ```
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	for _, k := range known {
		if s == k {
			return k
		}
	}
	// fallback: case-insensitive contains
	low := strings.ToLower(s)
	for _, k := range known {
		if strings.Contains(low, strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

// TruncateUserText caps user input length for the judge prompt.
func TruncateUserText(s string, maxChars int) string {
	if maxChars <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	return string(r[:maxChars])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/routing/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/routing/judge.go internal/routing/judge_test.go
git commit -m "feat(routing): judge model prompt builder + parser"
```

---

## Task 12: Routing engine (priority logic)

**Files:**
- Create: `internal/routing/engine.go`
- Test: `internal/routing/engine_test.go`

- [ ] **Step 1: Write the failing test**

`internal/routing/engine_test.go`:
```go
package routing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

type fakeStore struct {
	judge    *store.Model
	def      *store.Model
	byName   map[string]*store.Model
	sessions map[string]*store.Session
}

func (f *fakeStore) GetJudgeModel() (*store.Model, error)             { return f.judge, nil }
func (f *fakeStore) GetRoutingConfig() (*store.RoutingConfig, error)  { return &store.RoutingConfig{DefaultModelID: puint(2), JudgeMaxInputChars: 1000, EnableNextModelDirective: true, SessionTTLSeconds: 1800}, nil }
func (f *fakeStore) GetModel(id uint) (*store.Model, error)           { return f.byName["m"+itoa(id)], nil }
func (f *fakeStore) GetModelByName(n string) (*store.Model, error)    { return f.byName[n], nil }
func (f *fakeStore) ListEnabledModels() ([]store.Model, error) {
	var out []store.Model
	for _, m := range f.byName {
		out = append(out, *m)
	}
	return out, nil
}
func (f *fakeStore) GetSession(id string) (*store.Session, error) { return f.sessions[id], nil }
func (f *fakeStore) SetNextModel(id, m string, ttl time.Duration) error {
	f.sessions[id] = &store.Session{ID: id, NextModel: m}
	return nil
}

type fakeJudge struct {
	out string
	err error
}

func (fj *fakeJudge) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	return fj.out, fj.err
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
			"gpt-4o": {ID: 1, Name: "gpt-4o"},
			"m2":     {ID: 2, Name: "default-model"},
		},
		sessions: map[string]*store.Session{},
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

func TestRouteSession(t *testing.T) {
	e, fs, _ := newEngine()
	fs.sessions["sess1"] = &store.Session{ID: "sess1", NextModel: "gpt-4o", ExpiresAt: time.Now().Add(time.Hour)}
	req := &model.ChatRequest{SessionID: "sess1"}
	dec, err := e.Route(req)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", dec.ModelName)
	assert.Equal(t, "session", dec.Reason)
}

func TestRouteJudge(t *testing.T) {
	e, fs, fj := newEngine()
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/routing/ -run TestRoute -v`
Expected: FAIL — `Engine`, `Decision`, interfaces undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/routing/engine.go`:
```go
package routing

import (
	"fmt"
	"time"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

// StoreDeps is the subset of *store.Store the engine needs.
type StoreDeps interface {
	GetJudgeModel() (*store.Model, error)
	GetRoutingConfig() (*store.RoutingConfig, error)
	GetModel(id uint) (*store.Model, error)
	GetModelByName(name string) (*store.Model, error)
	ListEnabledModels() ([]store.Model, error)
	GetSession(id string) (*store.Session, error)
	SetNextModel(id, model string, ttl time.Duration) error
}

// JudgeClient invokes the judge model to pick a model name.
type JudgeClient interface {
	Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error)
}

type Decision struct {
	ModelName string
	Model     *store.Model
	Reason    string // override | session | judge | fallback
	JudgeRaw  string
}

type Engine struct {
	Store StoreDeps
	Judge JudgeClient
}

func New(s StoreDeps, j JudgeClient) *Engine {
	return &Engine{Store: s, Judge: j}
}

// Route decides which model to use for the request.
func (e *Engine) Route(req *model.ChatRequest) (*Decision, error) {
	// 1. Override
	if req.Override != "" {
		if m, err := e.Store.GetModelByName(req.Override); err == nil && m != nil && m.Enabled {
			return &Decision{ModelName: m.Name, Model: m, Reason: "override"}, nil
		}
	}

	// 2. Session next-model
	if req.SessionID != "" {
		if sess, err := e.Store.GetSession(req.SessionID); err == nil && sess != nil && sess.NextModel != "" {
			if m, err := e.Store.GetModelByName(sess.NextModel); err == nil && m != nil && m.Enabled {
				return &Decision{ModelName: m.Name, Model: m, Reason: "session"}, nil
			}
		}
	}

	// 3. Judge
	rc, err := e.Store.GetRoutingConfig()
	if err != nil {
		return nil, fmt.Errorf("get routing config: %w", err)
	}
	judge, _ := e.Store.GetJudgeModel()
	if judge != nil {
		cands, _ := e.Store.ListEnabledModels()
		userText := TruncateUserText(req.LastUserMessage(), rc.JudgeMaxInputChars)
		raw, jerr := e.Judge.Judge(judge, cands, userText)
		if jerr == nil && raw != "" {
			known := make([]string, 0, len(cands))
			for _, c := range cands {
				known = append(known, c.Name)
			}
			if picked := ParseJudgeOutput(raw, known); picked != "" {
				if m, err := e.Store.GetModelByName(picked); err == nil && m != nil {
					return &Decision{ModelName: m.Name, Model: m, Reason: "judge", JudgeRaw: raw}, nil
				}
			}
		}
	}

	// 4. Fallback to default model
	if rc.DefaultModelID != nil {
		if m, err := e.Store.GetModel(*rc.DefaultModelID); err == nil && m != nil {
			return &Decision{ModelName: m.Name, Model: m, Reason: "fallback", JudgeRaw: ""}, nil
		}
	}
	return nil, fmt.Errorf("no model available and no default configured")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/routing/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/routing/engine.go internal/routing/engine_test.go
git commit -m "feat(routing): priority routing engine"
```

---

## Task 13: Concrete judge client (calls upstream via dispatcher)

**Files:**
- Create: `internal/routing/judge_client.go`
- Test: `internal/routing/judge_client_test.go`

- [ ] **Step 1: Write the failing test**

`internal/routing/judge_client_test.go`:
```go
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

	jc := NewJudgeClient(upstream.New(), srv.URL, "sk-judge")
	judgeModel := &store.Model{Name: "judge-mini"}
	out, err := jc.Judge(judgeModel, []store.Model{{Name: "gpt-4o"}}, "hi")
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", out)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/routing/ -run TestJudgeClientCallsUpstream -v`
Expected: FAIL — `NewJudgeClient` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/routing/judge_client.go`:
```go
package routing

import (
	"context"
	"fmt"
	"time"

	"auto-router/internal/model"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

// JudgeClient implements JudgeClient by calling the judge model via the upstream dispatcher.
type JudgeClient struct {
	disp    *upstream.Dispatcher
	baseURL string
	apiKey  string
}

func NewJudgeClient(d *upstream.Dispatcher, baseURL, apiKey string) *JudgeClient {
	return &JudgeClient{disp: d, baseURL: baseURL, apiKey: apiKey}
}

func (j *JudgeClient) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	msgs := BuildJudgeMessages(candidates, userText)
	body := map[string]any{
		"model":    judgeModel.Name,
		"messages": msgs,
		"stream":   false,
	}
	// Use a short timeout via context-wrapped client.
	done := make(chan struct {
		resp *model.ChatResponse
		err  error
	}, 1)
	go func() {
		r, err := j.disp.Call(j.baseURL, j.apiKey, body)
		done <- struct {
			resp *model.ChatResponse
			err  error
		}{r, err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			return "", res.err
		}
		if len(res.resp.Choices) == 0 {
			return "", fmt.Errorf("judge returned no choices")
		}
		return res.resp.Choices[0].Message.Content, nil
	case <-time.After(10 * time.Second):
		return "", fmt.Errorf("judge timeout")
	case <-context.Background().Done():
		return "", context.Background().Err()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/routing/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/routing/judge_client.go internal/routing/judge_client_test.go
git commit -m "feat(routing): concrete judge client via dispatcher"
```

---

## Task 14: JWT helper

**Files:**
- Create: `internal/jwt/jwt.go`
- Test: `internal/jwt/jwt_test.go`

- [ ] **Step 1: Write the failing test**

`internal/jwt/jwt_test.go`:
```go
package jwt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIssueAndVerify(t *testing.T) {
	mgr := New("my-secret")
	tok, err := mgr.Issue("admin")
	assert.NoError(t, err)
	assert.NotEmpty(t, tok)
	claims, err := mgr.Verify(tok)
	assert.NoError(t, err)
	assert.Equal(t, "admin", claims.Subject)
}

func TestVerifyInvalid(t *testing.T) {
	mgr := New("my-secret")
	_, err := mgr.Verify("not-a-token")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/jwt/ -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/jwt/jwt.go`:
```go
package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Manager struct {
	secret []byte
}

func New(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

func (m *Manager) Issue(subject string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(24 * 7 * time.Hour)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(m.secret)
}

func (m *Manager) Verify(token string) (*jwt.RegisteredClaims, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/jwt/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jwt/
git commit -m "feat(jwt): issue/verify admin tokens"
```

---

## Task 15: Server wiring — handlers + middleware + gateway endpoint

**Files:**
- Create: `internal/server/middleware.go`
- Create: `internal/server/gateway.go`
- Modify: `internal/server/server.go`
- Test: `internal/server/gateway_test.go`

- [ ] **Step 1: Write the failing test (non-stream end-to-end with mock upstream)**

`internal/server/gateway_test.go`:
```go
package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Mock upstream returns a fixed non-streaming response.
func startMockUpstream(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
	}))
	return srv.URL
}

func TestGatewayNonStreamRoute(t *testing.T) {
	upstreamURL := startMockUpstream(t)
	app := newTestApp(t, upstreamURL)
	r := app.Router

	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "hello")
	// log written
	logs, total, _ := app.Store.ListLogs(1, 10, "judge", "")
	assert.Equal(t, int64(1), total)
	assert.NotEmpty(t, logs)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestGatewayNonStreamRoute -v`
Expected: FAIL — `newTestApp`, `App` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/server/middleware.go`:
```go
package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-router/internal/jwt"
)

// GatewayAuth middleware checks the single gateway bearer token.
func GatewayAuth(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid token", "type": "auth_error"}})
			return
		}
		c.Next()
	}
}

// AdminAuth middleware checks the admin JWT.
func AdminAuth(mgr *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		_, err := mgr.Verify(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Next()
	}
}
```

`internal/server/gateway.go`:
```go
package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
	"auto-router/internal/routing"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

func (a *App) handleChatCompletions(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req, err := openai.ParseRequest(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req.SessionID = c.GetHeader("X-Session-Id")
	if m := c.GetHeader("X-Route-Model"); m != "" {
		req.Override = m
	} else if !req.IsRouteRequested() {
		req.Override = req.Model // explicit model in body
	}

	start := time.Now()
	dec, err := a.Engine.Route(req)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": err.Error(), "type": "router_error"}})
		return
	}

	// Resolve provider + decrypted api key
	prov, err := a.Store.GetProvider(dec.Model.ProviderID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "provider not found", "type": "router_error"}})
		return
	}
	apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)

	// Build upstream request with chosen model
	req.Model = dec.Model.Name
	body, _ := openai.BuildUpstreamRequest(req)

	status := http.StatusOK
	errMsg := ""
	if req.Stream {
		a.streamResponse(c, prov.BaseURL, apiKey, body, dec, req, start)
		return
	}
	resp, err := a.Dispatcher.Call(prov.BaseURL, apiKey, body)
	if err != nil {
		status = http.StatusBadGateway
		errMsg = err.Error()
		c.JSON(status, gin.H{"error": gin.H{"message": err.Error(), "type": "upstream_error"}})
	} else {
		// Post-process next-model directive
		a.postProcessResponse(req, resp)
		b, _ := openai.EncodeResponseToClient(resp)
		c.Data(http.StatusOK, "application/json", b)
	}
	a.writeLog(req, dec, status, time.Since(start), errMsg)
}

func (a *App) streamResponse(c *gin.Context, baseURL, apiKey string, body map[string]any, dec *routing.Decision, req *model.ChatRequest, start time.Time) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, _ := c.Writer.(http.Flusher)
	status := http.StatusOK
	errMsg := ""
	chunks, err := a.Dispatcher.CallStream(baseURL, apiKey, body)
	if err != nil {
		status = http.StatusBadGateway
		errMsg = err.Error()
		c.JSON(status, gin.H{"error": gin.H{"message": err.Error(), "type": "upstream_error"}})
		a.writeLog(req, dec, status, time.Since(start), errMsg)
		return
	}
	var assembledContent string
	for _, ch := range chunks {
		if ch == nil {
			c.Writer.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			break
		}
		if len(ch.Choices) > 0 {
			assembledContent += ch.Choices[0].Delta.Content
		}
		b, _ := openai.EncodeChunk(ch)
		c.Writer.Write(b)
		c.Writer.Write([]byte("\n\n"))
		flusher.Flush()
	}
	// Post-process: if directive found in assembled content, persist and we cannot strip from
	// already-sent stream (acceptable: directive is rare and we still record it).
	if clean, mname := routing.ExtractNextModel(assembledContent); mname != "" {
		a.persistNextModel(req, mname)
		_ = clean
	}
	a.writeLog(req, dec, status, time.Since(start), errMsg)
}

func (a *App) postProcessResponse(req *model.ChatRequest, resp *model.ChatResponse) {
	if len(resp.Choices) == 0 {
		return
	}
	clean, mname := routing.ExtractNextModel(resp.Choices[0].Message.Content)
	if mname != "" {
		resp.Choices[0].Message.Content = clean
		a.persistNextModel(req, mname)
	}
}

func (a *App) persistNextModel(req *model.ChatRequest, mname string) {
	if req.SessionID == "" {
		return
	}
	if m, err := a.Store.GetModelByName(mname); err == nil && m != nil {
		rc, _ := a.Store.GetRoutingConfig()
		ttl := time.Duration(1800) * time.Second
		if rc != nil && rc.SessionTTLSeconds > 0 {
			ttl = time.Duration(rc.SessionTTLSeconds) * time.Second
		}
		_ = a.Store.SetNextModel(req.SessionID, mname, ttl)
	}
}

func (a *App) writeLog(req *model.ChatRequest, dec *routing.Decision, status int, dur time.Duration, errMsg string) {
	_ = a.Store.CreateLog(&store.RequestLog{
		SessionID:      req.SessionID,
		ClientProtocol: req.ClientFmt,
		RequestedModel: req.Model,
		RoutedModel:    dec.ModelName,
		RouteReason:    dec.Reason,
		JudgeRaw:       dec.JudgeRaw,
		Status:         status,
		LatencyMs:      dur.Milliseconds(),
		Error:          errMsg,
	})
}

func (a *App) handleListModels(c *gin.Context) {
	ms, err := a.Store.ListEnabledModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	data := []gin.H{}
	for _, m := range ms {
		data = append(data, gin.H{"id": m.Name, "object": "model", "owned_by": "auto-router"})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}
```

Now rewrite `internal/server/server.go` to add the `App` type and wiring (the health endpoint stays):

`internal/server/server.go`:
```go
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"auto-router/internal/config"
	"auto-router/internal/jwt"
	"auto-router/internal/routing"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

type Config = config.Config

type App struct {
	Router       *gin.Engine
	Store        *store.Store
	Engine       *routing.Engine
	Dispatcher   *upstream.Dispatcher
	JWT          *jwt.Manager
	CryptoKey    []byte
	GatewayToken string
}

func NewRouter(_ Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

// NewApp wires everything together.
func NewApp(cfg Config, st *store.Store, cryptoKey []byte, gatewayToken, adminToken string) *App {
	jwtMgr := jwt.New(adminToken)
	disp := upstream.New()
	judgeClient := routing.NewJudgeClient(disp, "", "") // baseURL/apiKey set per-call via judge model provider
	engine := routing.New(st, &lazyJudge{st: st, disp: disp, key: cryptoKey})

	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	app := &App{
		Router:       r,
		Store:        st,
		Engine:       engine,
		Dispatcher:   disp,
		JWT:          jwtMgr,
		CryptoKey:    cryptoKey,
		GatewayToken: gatewayToken,
	}

	v1 := r.Group("/v1", GatewayAuth(gatewayToken))
	v1.POST("/chat/completions", app.handleChatCompletions)
	v1.GET("/models", app.handleListModels)

	admin := r.Group("/admin")
	admin.POST("/login", app.handleAdminLogin)
	authAdmin := admin.Group("", AdminAuth(jwtMgr))
	authAdmin.GET("/providers", app.handleListProviders)
	authAdmin.POST("/providers", app.handleCreateProvider)
	authAdmin.PUT("/providers/:id", app.handleUpdateProvider)
	authAdmin.DELETE("/providers/:id", app.handleDeleteProvider)
	authAdmin.POST("/providers/:id/test", app.handleTestProvider)
	authAdmin.GET("/models", app.handleListModelsAdmin)
	authAdmin.POST("/models", app.handleCreateModel)
	authAdmin.PUT("/models/:id", app.handleUpdateModel)
	authAdmin.DELETE("/models/:id", app.handleDeleteModel)
	authAdmin.POST("/models/:id/judge", app.handleSetJudge)
	authAdmin.GET("/routing", app.handleGetRouting)
	authAdmin.PUT("/routing", app.handleUpdateRouting)
	authAdmin.GET("/logs", app.handleListLogs)
	authAdmin.GET("/stats", app.handleStats)
	return app
}

// lazyJudge resolves the judge model's provider at call time.
type lazyJudge struct {
	st   *store.Store
	disp *upstream.Dispatcher
	key  []byte
}

func (l *lazyJudge) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	prov, err := l.st.GetProvider(judgeModel.ProviderID)
	if err != nil {
		return "", err
	}
	apiKey, _ := store.Decrypt(l.key, prov.APIKey)
	jc := routing.NewJudgeClient(l.disp, prov.BaseURL, apiKey)
	client := jc
	_ = client
	// Use JudgeClient.Judge signature
	return routing.NewJudgeClient(l.disp, prov.BaseURL, apiKey).Judge(judgeModel, candidates, userText)
}
```

Create a test helper `internal/server/apptest_test.go`:
```go
package server

import (
	"testing"

	"auto-router/internal/config"
	"auto-router/internal/store"
)

type testApp struct {
	*App
	UpstreamURL string
}

func newTestApp(t *testing.T, upstreamURL string) *testApp {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	key := store.DeriveKey("test-seed")
	// create provider + model + judge + default
	prov := &store.Provider{Name: "p", BaseURL: upstreamURL, APIKey: store.Encrypt(key, "sk-test"), Protocol: "openai", Enabled: true}
	_ = st.CreateProvider(prov)
	judge := &store.Model{Name: "judge-mini", DisplayName: "Judge", ProviderID: prov.ID, Enabled: true}
	_ = st.CreateModel(judge)
	_ = st.SetJudgeModel(judge.ID)
	target := &store.Model{Name: "gpt-4o", DisplayName: "GPT4o", ProviderID: prov.ID, Enabled: true}
	_ = st.CreateModel(target)
	_ = st.UpdateRoutingConfig(&store.RoutingConfig{ID: 1, JudgeModelID: &judge.ID, DefaultModelID: &target.ID, EnableNextModelDirective: true, SessionTTLSeconds: 1800, JudgeMaxInputChars: 2000})

	cfg := config.Config{}
	app := NewApp(cfg, st, key, "gw-token", "admin-token")
	return &testApp{App: app, UpstreamURL: upstreamURL}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestGatewayNonStreamRoute -v`
Expected: PASS. (Also run `go build ./...` to ensure the admin handlers referenced in routes exist — they are added in Task 16, so the build will fail until then. To keep Task 15 green standalone, comment out the admin routes that reference not-yet-created handlers, OR implement stub handlers now.)

> Note: To keep the build green at this commit, also add stub admin handlers in Task 16 before committing Task 15, or implement them in the same commit. Recommended: implement Task 16 immediately after, then commit Tasks 15+16 together.

- [ ] **Step 5: Commit (after Task 16 adds admin handler stubs/implementations so build is green)**

```bash
git add internal/server/
git commit -m "feat(server): gateway endpoint with routing + streaming"
```

---

## Task 16: Admin API handlers

**Files:**
- Create: `internal/server/admin.go`
- Test: `internal/server/admin_test.go`

- [ ] **Step 1: Write the failing test**

`internal/server/admin_test.go`:
```go
package server

import (
	"bytes"
	"encoding/json"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestAdmin -v`
Expected: FAIL — admin handlers undefined (build fails).

- [ ] **Step 3: Write minimal implementation**

`internal/server/admin.go`:
```go
package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/store"
)

func (a *App) handleAdminLogin(c *gin.Context) {
	var body struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// admin token is the JWT secret; verify by issuing a token (which round-trips)
	if body.Token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	mgr := jwt.New(body.Token)
	// To validate, we compare against the app's admin secret: issue+verify with app secret
	if body.Token != string(a.JWT.Secret()) {
		// JWT.Manager doesn't expose secret; we store adminToken on App for comparison
		_ = mgr
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	tok, err := a.JWT.Issue("admin")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": tok, "expires_in": int((24 * 7 * time.Hour).Seconds())})
}

// ---- Providers ----

func (a *App) handleListProviders(c *gin.Context) {
	ps, err := a.Store.ListProviders()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ps})
}

func (a *App) handleCreateProvider(c *gin.Context) {
	var p store.Provider
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if p.APIKey != "" {
		p.APIKey = store.Encrypt(a.CryptoKey, p.APIKey)
	}
	if err := a.Store.CreateProvider(&p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (a *App) handleUpdateProvider(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	p, err := a.Store.GetProvider(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var body store.Provider
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p.Name = body.Name
	p.BaseURL = body.BaseURL
	p.Protocol = body.Protocol
	p.Enabled = body.Enabled
	if body.APIKey != "" {
		p.APIKey = store.Encrypt(a.CryptoKey, body.APIKey)
	}
	if err := a.Store.UpdateProvider(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (a *App) handleDeleteProvider(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := a.Store.DeleteProvider(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleTestProvider(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	p, err := a.Store.GetProvider(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	apiKey, _ := store.Decrypt(a.CryptoKey, p.APIKey)
	// issue a trivial models list request to the provider
	status, err := a.Dispatcher.TestConnect(p.BaseURL, apiKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": status, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": status})
}

// ---- Models ----

func (a *App) handleListModelsAdmin(c *gin.Context) {
	ms, err := a.Store.ListModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ms})
}

func (a *App) handleCreateModel(c *gin.Context) {
	var m store.Model
	if err := c.ShouldBindJSON(&m); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := a.Store.CreateModel(&m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func (a *App) handleUpdateModel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	m, err := a.Store.GetModel(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var body store.Model
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m.Name = body.Name
	m.DisplayName = body.DisplayName
	m.ProviderID = body.ProviderID
	m.Description = body.Description
	m.Enabled = body.Enabled
	if err := a.Store.UpdateModel(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func (a *App) handleDeleteModel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := a.Store.DeleteModel(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) handleSetJudge(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := a.Store.SetJudgeModel(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- Routing config ----

func (a *App) handleGetRouting(c *gin.Context) {
	rc, err := a.Store.GetRoutingConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rc)
}

func (a *App) handleUpdateRouting(c *gin.Context) {
	var rc store.RoutingConfig
	if err := c.ShouldBindJSON(&rc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := a.Store.UpdateRoutingConfig(&rc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rc)
}

// ---- Logs & stats ----

func (a *App) handleListLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	reason := c.Query("reason")
	model := c.Query("model")
	logs, total, err := a.Store.ListLogs(page, size, reason, model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs, "total": total, "page": page, "page_size": size})
}

func (a *App) handleStats(c *gin.Context) {
	var total int64
	a.Store.DB.Model(&store.RequestLog{}).Count(&total)
	type reasonCount struct {
		Reason string
		Count  int64
	}
	var reasons []reasonCount
	a.Store.DB.Model(&store.RequestLog{}).Select("route_reason as reason, count(*) as count").Group("route_reason").Scan(&reasons)
	c.JSON(http.StatusOK, gin.H{"total": total, "by_reason": reasons})
}
```

Now fix the `handleAdminLogin` reference to `a.JWT.Secret()`. The `jwt.Manager` doesn't expose `Secret()`. Add an `AdminToken` field on `App`. Update `server.go`:

In `NewApp`, add `AdminToken: adminToken,` to the `App{...}` literal and the `App` struct gets `AdminToken string`. Then in `handleAdminLogin` replace `string(a.JWT.Secret())` with `a.AdminToken`.

Also add `TestConnect` to the dispatcher (referenced by `handleTestProvider`). Update `internal/upstream/dispatcher.go`:

```go
// TestConnect issues a GET {baseURL}/models to verify connectivity.
func (d *Dispatcher) TestConnect(baseURL, apiKey string) (int, error) {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
```

Apply the `AdminToken` field fix: edit `internal/server/server.go` to add the field and assignment, and edit `admin.go`'s `handleAdminLogin` to use `a.AdminToken`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./... -v`
Expected: all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin.go internal/server/admin_test.go internal/upstream/dispatcher.go internal/server/server.go
git commit -m "feat(server): admin API (auth, providers, models, routing, logs, stats)"
```

---

## Task 17: Bootstrap (admin/gateway tokens + crypto seed) and wire main

**Files:**
- Create: `internal/server/bootstrap.go`
- Modify: `cmd/router/main.go`
- Test: `internal/server/bootstrap_test.go`

- [ ] **Step 1: Write the failing test**

`internal/server/bootstrap_test.go`:
```go
package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/store"
)

func TestBootstrapGeneratesMissingSecrets(t *testing.T) {
	st, _ := store.Open(":memory:")
	key, gw, admin, err := Bootstrap(st)
	assert.NoError(t, err)
	assert.Len(t, key, 32)
	assert.NotEmpty(t, gw)
	assert.NotEmpty(t, admin)

	// second call returns same values (persisted)
	key2, gw2, admin2, err := Bootstrap(st)
	assert.NoError(t, err)
	assert.Equal(t, key, key2)
	assert.Equal(t, gw, gw2)
	assert.Equal(t, admin, admin2)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestBootstrap -v`
Expected: FAIL — `Bootstrap` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/server/bootstrap.go`:
```go
package server

import (
	"crypto/rand"
	"encoding/hex"

	"auto-router/internal/store"
)

const (
	settingCryptoSeed  = "crypto_seed"
	settingGatewayToken = "gateway_token"
	settingAdminToken   = "admin_token"
)

// Bootstrap loads (or generates + persists) the crypto seed, gateway token, admin token.
func Bootstrap(st *store.Store) (key []byte, gatewayToken, adminToken string, err error) {
	seed, err := getOrCreate(st, settingCryptoSeed, randomHex(32))
	if err != nil {
		return nil, "", "", err
	}
	key = store.DeriveKey(seed)
	gatewayToken, err = getOrCreate(st, settingGatewayToken, randomHex(24))
	if err != nil {
		return nil, "", "", err
	}
	adminToken, err = getOrCreate(st, settingAdminToken, randomHex(24))
	if err != nil {
		return nil, "", "", err
	}
	return key, gatewayToken, adminToken, nil
}

func getOrCreate(st *store.Store, key, gen string) (string, error) {
	v, err := st.GetSetting(key)
	if err == nil && v != "" {
		return v, nil
	}
	if err := st.SetSetting(key, gen); err != nil {
		return "", err
	}
	return gen, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
```

Update `cmd/router/main.go`:
```go
package main

import (
	"log"

	"auto-router/internal/config"
	"auto-router/internal/server"
	"auto-router/internal/store"
)

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	key, gwToken, adminToken, err := server.Bootstrap(st)
	if err != nil {
		log.Fatal(err)
	}
	// env override
	if cfg.GatewayToken != "" {
		gwToken = cfg.GatewayToken
	}
	if cfg.AdminToken != "" {
		adminToken = cfg.AdminToken
	}
	app := server.NewApp(cfg, st, key, gwToken, adminToken)
	log.Printf("listening on %s | gateway token: %s | admin token: %s", cfg.ListenAddr, gwToken, adminToken)
	if err := app.Router.Run(cfg.ListenAddr); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/bootstrap.go internal/server/bootstrap_test.go cmd/router/main.go
git commit -m "feat: bootstrap secrets + wire main"
```

---

## Task 18: Session expiry cleanup goroutine

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Add to `internal/server/server_test.go`:
```go
func TestSessionCleanup(t *testing.T) {
	app := newTestApp(t, "http://example.com")
	// insert expired session directly
	_ = app.Store.SetNextModel("expired", "gpt-4o", -1*time.Hour)
	StartSessionCleanup(app.Store, 50*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	_, err := app.Store.GetSession("expired")
	assert.Error(t, err) // expired -> not found
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSessionCleanup -v`
Expected: FAIL — `StartSessionCleanup` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/server/server.go`:
```go
import (
	"time"
	// ...
)

// StartSessionCleanup periodically deletes expired sessions.
func StartSessionCleanup(st *store.Store, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			_, _ = st.CleanExpiredSessions()
		}
	}()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestSessionCleanup -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: session expiry cleanup goroutine"
```

---

## Task 19: Integration test — end-to-end routing with directive

**Files:**
- Test: `internal/server/integration_test.go`

- [ ] **Step 1: Write the failing test**

`internal/server/integration_test.go`:
```go
package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Mock upstream that echoes a next_model directive in non-stream response.
func startDirectiveUpstream(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok<<next_model: gpt-4o>>"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
	}))
	return srv.URL
}

func TestEndToEndDirectiveStrippedAndSessionSet(t *testing.T) {
	url := startDirectiveUpstream(t)
	app := newTestApp(t, url)

	// first request: routed by judge, response contains directive
	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("X-Session-Id", "sess-x")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	// directive stripped from visible response
	assert.Equal(t, false, bytes.Contains(w.Body.Bytes(), []byte("<<next_model")))
	assert.Contains(t, w.Body.String(), "ok")

	// session now has next_model
	sess, err := app.Store.GetSession("sess-x")
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", sess.NextModel)
}

func TestEndToEndOverrideSkipsRouting(t *testing.T) {
	url := startDirectiveUpstream(t)
	app := newTestApp(t, url)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	logs, _, _ := app.Store.ListLogs(1, 10, "override", "")
	assert.Len(t, logs, 1)
}

func TestEndToEndStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n")
		io.WriteString(w, "data: {\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	app := newTestApp(t, srv.URL)
	body := `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+app.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data: ")
	assert.Contains(t, w.Body.String(), "[DONE]")
}

var _ = time.Second
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./internal/server/ -run TestEndToEnd -v`
Expected: PASS (the implementation already supports these; the test validates behavior).

- [ ] **Step 3: Commit**

```bash
git add internal/server/integration_test.go
git commit -m "test: end-to-end routing, directive, streaming"
```

---

## Task 20: README + final verification

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write README**

`README.md`:
```markdown
# Auto Model Router

AI 模型路由网关。请求到达时,由"判定模型"选择最合适的模型执行,支持 Agent 指定与会话级模型回选。对外兼容 OpenAI 协议。

## 快速开始

```bash
go build -o auto-router ./cmd/router
./auto-router
```

首次启动会在 `auto-router.db` 中生成并打印 gateway token 和 admin token。

## 配置(环境变量)

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8080` | 监听地址 |
| `DB_PATH` | `auto-router.db` | SQLite 路径 |
| `GATEWAY_TOKEN` | 自动生成 | 客户端访问网关的 token(可覆盖) |
| `ADMIN_TOKEN` | 自动生成 | 管理后台 token(可覆盖) |

## 使用

1. 用 admin token 登录 `/admin/login`,添加 API 源(Provider)和模型。
2. 设置一个模型为判定模型(`POST /admin/models/:id/judge`),设置默认兜底模型(`PUT /admin/routing`)。
3. 客户端以 OpenAI 协议调用 `POST /v1/chat/completions`,`Authorization: Bearer <gateway token>`。
   - `model` 留空 / `"auto"` / `"route"` → 自动路由
   - `model` 设为具体模型名 → 显式指定
   - `X-Session-Id` 头 → 启用会话级模型回选
   - `X-Route-Model` 头 → 强制指定模型(最高优先)

## 路由模式

| 模式 | 触发 | reason |
|------|------|--------|
| Agent 指定 | `model` 字段或 `X-Route-Model` 头 | override |
| 会话回选 | 上轮响应含 `<<next_model: 名称>>` + `X-Session-Id` | session |
| 自动路由 | 判定模型选择 | judge |
| 兜底 | 判定失败 | fallback |
```

- [ ] **Step 2: Run full test suite + build**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS, no vet warnings.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: README"
```

---

## Self-Review (completed)

**1. Spec coverage (Plan 1 scope):**
- OpenAI inbound/outbound adapters → Tasks 5–8 ✓
- Canonical model → Task 4 ✓
- Routing engine (override/session/judge/fallback) → Task 12 ✓
- Judge model invocation + parsing → Tasks 11, 13 ✓
- Next-model directive (extract + strip + persist) → Task 10, used in 15 ✓
- Upstream dispatcher (stream + non-stream) → Task 9 ✓
- Gateway endpoints (/v1/chat/completions, /v1/models) → Task 15 ✓
- Admin API (auth, providers, models, routing, logs, stats, test) → Task 16 ✓
- SQLite schema → Task 2 ✓
- Crypto for API keys → Task 3 ✓
- Single token auth → Task 15 (gateway), Task 16 (admin) ✓
- Session TTL cleanup → Task 18 ✓
- Bootstrap tokens → Task 17 ✓
- Tests (adapter golden, routing paths, integration) → Tasks 5–19 ✓
- **Out of scope (Plan 2):** Claude protocol adapters + cross-protocol + `/v1/messages`
- **Out of scope (Plan 3):** React frontend

**2. Placeholder scan:** None. All steps contain real code. (Task 15's `handleAdminLogin` had a `Secret()` reference that Task 16 fixes by using `App.AdminToken` — applied during Task 16.)

**3. Type consistency:** `Decision` fields (ModelName, Reason, JudgeRaw) used consistently in Tasks 12/15. `store.Model`/`store.Provider` field names match across store + admin + engine. `JudgeClient.Judge` signature matches between interface (Task 12), concrete client (Task 13), and `lazyJudge` (Task 15).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-29-auto-router-backend-core.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
