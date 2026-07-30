package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/adapter/claude"
	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
	"auto-router/internal/routing"
	"auto-router/internal/store"
)

// chunkEncoder encodes canonical chunks into the client's SSE format.
type chunkEncoder interface {
	EncodeChunk(ch *model.Chunk) []byte
	Finish() []byte
}

// openaiChunkEncoder is stateless; each chunk is one data: line + \n\n.
type openaiChunkEncoder struct{}

func (openaiChunkEncoder) EncodeChunk(ch *model.Chunk) []byte {
	b, _ := openai.EncodeChunk(ch)
	return append(b, '\n', '\n')
}
func (openaiChunkEncoder) Finish() []byte { return []byte("data: [DONE]\n\n") }

// claudeChunkEncoder wraps claude.StreamEncoder.
type claudeChunkEncoder struct{ enc *claude.StreamEncoder }

func (e *claudeChunkEncoder) EncodeChunk(ch *model.Chunk) []byte { return e.enc.EncodeChunk(ch) }
func (e *claudeChunkEncoder) Finish() []byte                     { return e.enc.Finish() }

// writeGatewayError writes an error in the client's protocol format.
func writeGatewayError(c *gin.Context, status int, clientFmt, msg, errType string) {
	if clientFmt == "claude" {
		c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": errType, "message": msg}})
	} else {
		c.JSON(status, gin.H{"error": gin.H{"message": msg, "type": errType}})
	}
}

// handleChat is the shared handler for both /v1/chat/completions and /v1/messages.
func (a *App) handleChat(c *gin.Context, clientFmt string, parseInbound func(map[string]any) (*model.ChatRequest, error)) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		writeGatewayError(c, http.StatusBadRequest, clientFmt, err.Error(), "invalid_request_error")
		return
	}
	req, err := parseInbound(raw)
	if err != nil {
		writeGatewayError(c, http.StatusBadRequest, clientFmt, err.Error(), "invalid_request_error")
		return
	}
	req.SessionID = c.GetHeader("X-Session-Id")
	if m := c.GetHeader("X-Route-Model"); m != "" {
		req.Override = m
	} else if !req.IsRouteRequested() {
		req.Override = req.Model
	}

	start := time.Now()
	dec, err := a.Engine.Route(req)
	if err != nil {
		writeGatewayError(c, http.StatusServiceUnavailable, clientFmt, err.Error(), "router_error")
		return
	}

	requestedModel := req.Model

	prov, err := a.Store.GetProvider(dec.Model.ProviderID)
	if err != nil {
		writeGatewayError(c, http.StatusServiceUnavailable, clientFmt, "provider not found", "router_error")
		return
	}
	apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)

	req.Model = dec.Model.Name

	// Build upstream body based on UPSTREAM protocol
	var body map[string]any
	if prov.Protocol == "claude" {
		body, _ = claude.BuildUpstreamRequest(req)
	} else {
		body, _ = openai.BuildUpstreamRequest(req)
	}

	status := http.StatusOK
	errMsg := ""
	if req.Stream {
		a.streamResponse(c, prov.BaseURL, apiKey, prov.Protocol, body, dec, req, requestedModel, start, prov.RetryMax, prov.RetryBackoffMs)
		return
	}
	resp, retryCount, err := a.Dispatcher.CallWithRetry(c.Request.Context(), prov.BaseURL, apiKey, prov.Protocol, body, prov.RetryMax, prov.RetryBackoffMs)
	if err != nil {
		status = http.StatusBadGateway
		errMsg = err.Error()
		writeGatewayError(c, status, clientFmt, err.Error(), "upstream_error")
	} else {
		// Encode response based on CLIENT protocol
		var b []byte
		if req.ClientFmt == "claude" {
			b, _ = claude.EncodeResponseToClient(resp)
		} else {
			b, _ = openai.EncodeResponseToClient(resp)
		}
		c.Data(http.StatusOK, "application/json", b)
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount)
}

func (a *App) handleChatCompletions(c *gin.Context) {
	a.handleChat(c, "openai", openai.ParseRequest)
}

func (a *App) handleMessages(c *gin.Context) {
	a.handleChat(c, "claude", claude.ParseRequest)
}

func (a *App) streamResponse(c *gin.Context, baseURL, apiKey, protocol string, body map[string]any, dec *routing.Decision, req *model.ChatRequest, requestedModel string, start time.Time, retryMax, backoffMs int) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, _ := c.Writer.(http.Flusher)
	status := http.StatusOK
	errMsg := ""

	// Choose encoder based on CLIENT protocol
	var enc chunkEncoder
	if req.ClientFmt == "claude" {
		enc = &claudeChunkEncoder{enc: claude.NewStreamEncoder(dec.ModelName)}
	} else {
		enc = openaiChunkEncoder{}
	}

	retryCount, streamErr := a.Dispatcher.CallStreamWithRetry(baseURL, apiKey, protocol, body, retryMax, backoffMs, func(ch *model.Chunk) error {
		if ch == nil {
			c.Writer.Write(enc.Finish())
			flusher.Flush()
			return nil
		}
		c.Writer.Write(enc.EncodeChunk(ch))
		flusher.Flush()
		return nil
	})
	if streamErr != nil {
		status = http.StatusBadGateway
		errMsg = streamErr.Error()
		writeGatewayError(c, status, req.ClientFmt, streamErr.Error(), "upstream_error")
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount)
}

func (a *App) writeLog(req *model.ChatRequest, dec *routing.Decision, requestedModel string, status int, dur time.Duration, errMsg string, retryCount int) {
	_ = a.Store.CreateLog(&store.RequestLog{
		SessionID:      req.SessionID,
		ClientProtocol: req.ClientFmt,
		RequestedModel: requestedModel,
		RoutedModel:    dec.ModelName,
		RouteReason:    dec.Reason,
		JudgeRaw:       dec.JudgeRaw,
		Status:         status,
		LatencyMs:      dur.Milliseconds(),
		Error:          errMsg,
		RetryCount:     retryCount,
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
