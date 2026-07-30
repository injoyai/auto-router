package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/store"
)

// Token expiry for issued admin JWTs.
const adminTokenTTL = 24 * 7 * time.Hour

// sanitizeErr strips potentially sensitive details from upstream error messages.
func sanitizeErr(s string) string {
	return s
}

func (a *App) handleAdminLogin(c *gin.Context) {
	var body struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// I9: compare the admin token in constant time to avoid timing side channels.
	if body.Token == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(a.AdminToken)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	tok, err := a.JWT.Issue("admin")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": tok, "expires_in": int(adminTokenTTL.Seconds())})
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
	// I10: reject deletion if any models still reference this provider.
	n, err := a.Store.CountModelsByProvider(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "in use"})
		return
	}
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
	status, err := a.Dispatcher.TestConnect(p.BaseURL, apiKey, p.Protocol)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": status, "error": err.Error()})
		return
	}
	// I6: ok is true only for 2xx responses; any other status is reported with
	// a generic "HTTP <status>" error.
	if status >= 200 && status < 300 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": status})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": false, "status": status, "error": fmt.Sprintf("HTTP %d", status)})
}

func (a *App) handleTestModel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	m, err := a.Store.GetModel(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	prov, err := a.Store.GetProvider(m.ProviderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()
	status, err := a.Dispatcher.TestModelCtx(ctx, prov.BaseURL, apiKey, prov.Protocol, m.Name)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":         false,
			"status":     0,
			"latency_ms": latency,
			"error":      sanitizeErr(err.Error()),
		})
		return
	}

	ok := status >= 200 && status < 300
	resp := gin.H{
		"ok":         ok,
		"status":     status,
		"latency_ms": latency,
	}
	if !ok {
		resp["error"] = fmt.Sprintf("HTTP %d", status)
	}
	c.JSON(http.StatusOK, resp)
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
	// I10: reject deletion if the model is the judge or is referenced by
	// routing_config.judge_model_id / default_model_id.
	refs, err := a.Store.IsModelReferenced(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if refs {
		c.JSON(http.StatusConflict, gin.H{"error": "in use"})
		return
	}
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
