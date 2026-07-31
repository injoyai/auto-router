package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/adapter/claude"
	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
	"auto-router/internal/store"
)

// Token expiry for issued admin JWTs.
const adminTokenTTL = 24 * 7 * time.Hour

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

// providerInput 用于接收创建/更新请求。store.Provider.APIKey 标记 json:"-"
// 以确保响应中不暴露密钥，但也导致 ShouldBindJSON 无法接收 api_key 字段。
// 因此用独立输入结构体接收，再映射到 store.Provider。
type providerInput struct {
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Protocol       string `json:"protocol"`
	Enabled        bool   `json:"enabled"`
	RetryMax       int    `json:"retry_max"`
	RetryBackoffMs int    `json:"retry_backoff_ms"`
	ProxyURL       string `json:"proxy_url"`
}

func (a *App) handleListProviders(c *gin.Context) {
	ps, err := a.Store.ListProviders()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range ps {
		ps[i].HasAPIKey = ps[i].APIKey != ""
		if ps[i].APIKey != "" {
			ps[i].APIKeyPlain, _ = store.Decrypt(a.CryptoKey, ps[i].APIKey)
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": ps})
}

func (a *App) handleCreateProvider(c *gin.Context) {
	var in providerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p := store.Provider{
		Name:           in.Name,
		BaseURL:        in.BaseURL,
		Protocol:       in.Protocol,
		Enabled:        in.Enabled,
		RetryMax:       in.RetryMax,
		RetryBackoffMs: in.RetryBackoffMs,
		ProxyURL:       in.ProxyURL,
	}
	if in.APIKey != "" {
		p.APIKey = store.Encrypt(a.CryptoKey, in.APIKey)
	}
	if err := a.Store.CreateProvider(&p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p.HasAPIKey = p.APIKey != ""
	p.APIKeyPlain = in.APIKey
	c.JSON(http.StatusOK, p)
}

func (a *App) handleUpdateProvider(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	p, err := a.Store.GetProvider(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var in providerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p.Name = in.Name
	p.BaseURL = in.BaseURL
	p.Protocol = in.Protocol
	p.Enabled = in.Enabled
	p.RetryMax = in.RetryMax
	p.RetryBackoffMs = in.RetryBackoffMs
	p.ProxyURL = in.ProxyURL
	if in.APIKey != "" {
		p.APIKey = store.Encrypt(a.CryptoKey, in.APIKey)
	}
	if err := a.Store.UpdateProvider(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p.HasAPIKey = p.APIKey != ""
	if in.APIKey != "" {
		p.APIKeyPlain = in.APIKey
	} else if p.APIKey != "" {
		p.APIKeyPlain, _ = store.Decrypt(a.CryptoKey, p.APIKey)
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
	start := time.Now()
	status, respBody, err := a.Dispatcher.TestConnect(p.BaseURL, apiKey, p.Protocol, p.ProxyURL)
	latency := time.Since(start).Milliseconds()

	// Persist a test log row so provider connectivity checks show up in the
	// logs page alongside normal request logs (route_reason="test").
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else if status < 200 || status >= 300 {
		errMsg = fmt.Sprintf("HTTP %d: %s", status, respBody)
	}
	_ = a.Store.CreateLog(&store.RequestLog{
		SessionID:      fmt.Sprintf("test-prov-%d", p.ID),
		ClientProtocol: p.Protocol,
		RequestedModel: "",
		RoutedModel:    p.Name,
		RouteReason:    "test",
		Status:         status,
		LatencyMs:      latency,
		Error:          errMsg,
	})

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": status, "error": err.Error()})
		return
	}
	// I6: ok is true only for 2xx responses; any other status is reported with
	// a generic "HTTP <status>" error plus the upstream body for diagnostics.
	if status >= 200 && status < 300 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": status, "latency_ms": latency})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": false, "status": status, "latency_ms": latency, "error": fmt.Sprintf("HTTP %d: %s", status, respBody)})
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
	status, respBody, err := a.Dispatcher.TestModelCtx(ctx, prov.BaseURL, apiKey, prov.Protocol, prov.ProxyURL, m.Name)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":         false,
			"status":     status,
			"latency_ms": latency,
			"error":      err.Error(),
		})
		return
	}

	ok := status >= 200 && status < 300
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else if !ok {
		errMsg = fmt.Sprintf("HTTP %d: %s", status, respBody)
	}

	// Parse the upstream response to extract token usage and the model's
	// reply content for richer diagnostics. Falls back to zero values when the
	// body cannot be parsed (e.g. truncated or non-JSON error bodies).
	usage, reply := parseTestResponse(prov.Protocol, respBody)

	// Persist a test log row so model connectivity checks show up in the
	// logs page alongside normal request logs (route_reason="test").
	_ = a.Store.CreateLog(&store.RequestLog{
		SessionID:        fmt.Sprintf("test-model-%d", m.ID),
		ClientProtocol:   prov.Protocol,
		RequestedModel:   m.Name,
		RoutedModel:      m.Name,
		RouteReason:      "test",
		Status:           status,
		LatencyMs:        latency,
		Error:            errMsg,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	})
	resp := gin.H{
		"ok":         ok,
		"status":     status,
		"latency_ms": latency,
	}
	if !ok {
		resp["error"] = errMsg
	}
	if usage.TotalTokens > 0 {
		resp["usage"] = gin.H{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	}
	if reply != "" {
		resp["reply"] = reply
	}
	c.JSON(http.StatusOK, resp)
}

// parseTestResponse extracts token usage and the assistant's reply content
// from an upstream test response body. Returns zero values when the body
// cannot be parsed (truncated, non-JSON, or error responses).
func parseTestResponse(protocol, body string) (model.Usage, string) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return model.Usage{}, ""
	}
	var resp *model.ChatResponse
	var err error
	if protocol == "claude" {
		resp, err = claude.ParseResponse(raw)
	} else {
		resp, err = openai.ParseResponse(raw)
	}
	if err != nil || resp == nil {
		return model.Usage{}, ""
	}
	reply := ""
	if len(resp.Choices) > 0 {
		reply = resp.Choices[0].Message.Content
	}
	return resp.Usage, reply
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

// ---- Routing config (includes gateway token) ----

func (a *App) handleGetRouting(c *gin.Context) {
	rc, err := a.Store.GetRoutingConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":                  rc.ID,
		"judge_group_id":      rc.JudgeGroupID,
		"default_group_id":    rc.DefaultGroupID,
		"judge_max_input_chars": rc.JudgeMaxInputChars,
		"gateway_token":       a.GatewayTokenValue(),
	})
}

func (a *App) handleUpdateRouting(c *gin.Context) {
	var body struct {
		JudgeGroupID       *uint  `json:"judge_group_id"`
		DefaultGroupID     *uint  `json:"default_group_id"`
		JudgeMaxInputChars int    `json:"judge_max_input_chars"`
		GatewayToken       string `json:"gateway_token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.JudgeGroupID != nil {
		g, err := a.Store.GetModelGroup(*body.JudgeGroupID)
		if err != nil || g == nil || !g.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "judge group not found or disabled"})
			return
		}
	}
	if body.DefaultGroupID != nil {
		g, err := a.Store.GetModelGroup(*body.DefaultGroupID)
		if err != nil || g == nil || !g.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "default group not found or disabled"})
			return
		}
	}
	rc := store.RoutingConfig{
		ID:                 1,
		JudgeGroupID:       body.JudgeGroupID,
		DefaultGroupID:     body.DefaultGroupID,
		JudgeMaxInputChars: body.JudgeMaxInputChars,
	}
	if err := a.Store.UpdateRoutingConfig(&rc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if body.GatewayToken != "" {
		if err := a.SetGatewayToken(body.GatewayToken); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"id":                  rc.ID,
		"judge_group_id":      rc.JudgeGroupID,
		"default_group_id":    rc.DefaultGroupID,
		"judge_max_input_chars": rc.JudgeMaxInputChars,
		"gateway_token":       a.GatewayTokenValue(),
	})
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

	totalTokens, _ := a.Store.TokenStatsTotal()
	byModel, _ := a.Store.TokenStatsByModel()
	byProvider, _ := a.Store.TokenStatsByProvider()

	c.JSON(http.StatusOK, gin.H{
		"total":       total,
		"by_reason":   reasons,
		"tokens":      gin.H{"total": totalTokens, "prompt": tokenSumPrompt(byModel), "completion": tokenSumCompletion(byModel)},
		"by_model":    byModel,
		"by_provider": byProvider,
	})
}

// tokenSumPrompt sums PromptTokens across rows.
func tokenSumPrompt(rows []store.TokenStatRow) int64 {
	var s int64
	for _, r := range rows {
		s += r.PromptTokens
	}
	return s
}

// tokenSumCompletion sums CompletionTokens across rows.
func tokenSumCompletion(rows []store.TokenStatRow) int64 {
	var s int64
	for _, r := range rows {
		s += r.CompletionTokens
	}
	return s
}

// ---- Model Groups (queues) ----

type groupInput struct {
	Name    string `json:"name"`
	Remark  string `json:"remark"`
	Enabled bool   `json:"enabled"`
}

func (a *App) handleListGroups(c *gin.Context) {
	gs, err := a.Store.ListModelGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type groupWithCount struct {
		store.ModelGroup
		ItemCount int64 `json:"item_count"`
	}
	out := make([]groupWithCount, 0, len(gs))
	for _, g := range gs {
		var cnt int64
		a.Store.DB.Model(&store.ModelGroupItem{}).Where("group_id = ?", g.ID).Count(&cnt)
		out = append(out, groupWithCount{ModelGroup: g, ItemCount: cnt})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) handleCreateGroup(c *gin.Context) {
	var in groupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g := store.ModelGroup{Name: in.Name, Remark: in.Remark, Enabled: in.Enabled}
	if err := a.Store.CreateModelGroup(&g); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (a *App) handleUpdateGroup(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	g, err := a.Store.GetModelGroup(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var in groupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.Name = in.Name
	g.Remark = in.Remark
	g.Enabled = in.Enabled
	if err := a.Store.UpdateModelGroup(g); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (a *App) handleDeleteGroup(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	// Guard against deleting the default queue. GetRoutingConfig may return an
	// error or nil (e.g. empty DB), so check both before dereferencing.
	rc, err := a.Store.GetRoutingConfig()
	if err == nil && rc != nil && rc.DefaultGroupID != nil && *rc.DefaultGroupID == uint(id) {
		c.JSON(http.StatusConflict, gin.H{"error": "in use as default"})
		return
	}
	if err := a.Store.DeleteModelGroup(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type groupItemOut struct {
	ID       uint         `json:"id"`
	GroupID  uint         `json:"group_id"`
	ModelID  uint         `json:"model_id"`
	Position int          `json:"position"`
	Model    *store.Model `json:"model"`
}

func (a *App) handleListGroupItems(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	items, err := a.Store.GetGroupItemsOrdered(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]groupItemOut, 0, len(items))
	for _, it := range items {
		m, _ := a.Store.GetModel(it.ModelID)
		out = append(out, groupItemOut{ID: it.ID, GroupID: it.GroupID, ModelID: it.ModelID, Position: it.Position, Model: m})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) handleReplaceGroupItems(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		Items []uint `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := a.Store.ReplaceGroupItems(uint(id), body.Items); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
