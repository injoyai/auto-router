package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
	"auto-router/internal/routing"
	"auto-router/internal/store"
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
	// Post-process: if directive found in assembled content, persist it. We cannot
	// strip from the already-sent stream (acceptable: directive is rare and we still
	// record it).
	if _, mname := routing.ExtractNextModel(assembledContent); mname != "" {
		a.persistNextModel(req, mname)
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
