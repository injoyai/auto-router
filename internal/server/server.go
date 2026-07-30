package server

import (
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"auto-router/internal/config"
	"auto-router/internal/jwt"
	"auto-router/internal/routing"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

type Config = config.Config

// App is the assembled application: HTTP router plus all wired dependencies.
type App struct {
	Router       *gin.Engine
	Store        *store.Store
	Engine       *routing.Engine
	Dispatcher   *upstream.Dispatcher
	JWT          *jwt.Manager
	CryptoKey    []byte
	GatewayToken string
	AdminToken   string

	gwMu sync.RWMutex
}

// GatewayTokenValue returns the current gateway token in a thread-safe manner.
func (a *App) GatewayTokenValue() string {
	a.gwMu.RLock()
	defer a.gwMu.RUnlock()
	return a.GatewayToken
}

// SetGatewayToken updates the in-memory gateway token (thread-safe) and
// persists it to the settings table so it survives restarts.
func (a *App) SetGatewayToken(token string) error {
	a.gwMu.Lock()
	a.GatewayToken = token
	a.gwMu.Unlock()
	return a.Store.SetSetting(settingGatewayToken, token)
}

// NewRouter builds a minimal gin engine with just the /health endpoint.
// It is kept for backwards compatibility with the Task 1 test.
func NewRouter(_ Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

// NewApp wires everything together: store, routing engine, dispatcher, JWT,
// and registers all gateway + admin routes on a fresh gin engine.
func NewApp(cfg Config, st *store.Store, cryptoKey []byte, gatewayToken, adminToken string) *App {
	jwtMgr := jwt.New(adminToken)
	disp := upstream.New()
	engine := routing.New(st, &lazyJudge{st: st, disp: disp, key: cryptoKey})

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// Dev mode: permissive CORS so the Vite dev server can call the backend.
	if cfg.DevMode {
		r.Use(func(c *gin.Context) {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
		})
	}

	app := &App{
		Router:       r,
		Store:        st,
		Engine:       engine,
		Dispatcher:   disp,
		JWT:          jwtMgr,
		CryptoKey:    cryptoKey,
		GatewayToken: gatewayToken,
		AdminToken:   adminToken,
	}

	v1 := r.Group("/v1", GatewayAuth(app.GatewayTokenValue))
	v1.POST("/chat/completions", app.handleChatCompletions)
	v1.POST("/messages", app.handleMessages)
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
	authAdmin.POST("/models/:id/test", app.handleTestModel)
	authAdmin.GET("/routing", app.handleGetRouting)
	authAdmin.PUT("/routing", app.handleUpdateRouting)
	authAdmin.GET("/logs", app.handleListLogs)
	authAdmin.GET("/stats", app.handleStats)
	return app
}

// lazyJudge resolves the judge model's provider + API key at call time so the
// routing engine does not need to know provider credentials up front.
type lazyJudge struct {
	st   *store.Store
	disp *upstream.Dispatcher
	key  []byte
}

// Compile-time guarantee that *lazyJudge satisfies routing.JudgeClient.
var _ routing.JudgeClient = (*lazyJudge)(nil)

func (l *lazyJudge) Judge(judgeModel *store.Model, candidates []store.Model, userText string) (string, error) {
	prov, err := l.st.GetProvider(judgeModel.ProviderID)
	if err != nil {
		return "", err
	}
	apiKey, _ := store.Decrypt(l.key, prov.APIKey)
	return routing.NewJudgeClient(l.disp, prov.BaseURL, apiKey, prov.Protocol).Judge(judgeModel, candidates, userText)
}

// ServeSPA registers a NoRoute handler to serve the embedded React SPA.
// webFS is the embedded filesystem from //go:embed.
func (a *App) ServeSPA(webFS fs.FS) {
	a.Router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/admin") || path == "/health" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		// Try serving the exact file; fall back to index.html for SPA routing.
		f, err := webFS.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			c.FileFromFS("/", http.FS(webFS))
			return
		}
		f.Close()
		c.FileFromFS(path, http.FS(webFS))
	})
}
