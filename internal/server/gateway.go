package server

import (
	"fmt"
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

	if req.Stream {
		a.streamResponse(c, dec, req, requestedModel, start)
		return
	}

	// Non-streaming: iterate the chain; any failure fails over to the next model.
	// Each model still uses its provider's RetryMax for retryable errors.
	status := http.StatusOK
	errMsg := ""
	var resp *model.ChatResponse
	var retryCount int
	var lastErr error
	for i, m := range dec.Models {
		dec.ServedModel = m.Name
		prov, perr := a.Store.GetProvider(m.ProviderID)
		if perr != nil {
			lastErr = fmt.Errorf("provider not found for model %s", m.Name)
			continue
		}
		apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)
		req.Model = m.Name
		var body map[string]any
		if prov.Protocol == "claude" {
			body, _ = claude.BuildUpstreamRequest(req)
		} else {
			body, _ = openai.BuildUpstreamRequest(req)
		}
		resp, retryCount, err = a.Dispatcher.CallWithRetry(c.Request.Context(), prov.BaseURL, apiKey, prov.Protocol, prov.ProxyURL, body, prov.RetryMax, prov.RetryBackoffMs)
		if err == nil {
			dec.FailoverCount = i
			break
		}
		lastErr = err
	}
	if resp == nil && lastErr != nil {
		status = http.StatusBadGateway
		errMsg = lastErr.Error()
		writeGatewayError(c, status, clientFmt, lastErr.Error(), "upstream_error")
	} else {
		var b []byte
		if req.ClientFmt == "claude" {
			b, _ = claude.EncodeResponseToClient(resp)
		} else {
			b, _ = openai.EncodeResponseToClient(resp)
		}
		c.Data(http.StatusOK, "application/json", b)
	}
	var usage *model.Usage
	if resp != nil {
		usage = &resp.Usage
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount, usage)
}

func (a *App) handleChatCompletions(c *gin.Context) {
	a.handleChat(c, "openai", openai.ParseRequest)
}

func (a *App) handleMessages(c *gin.Context) {
	a.handleChat(c, "claude", claude.ParseRequest)
}

func (a *App) streamResponse(c *gin.Context, dec *routing.Decision, req *model.ChatRequest, requestedModel string, start time.Time) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, _ := c.Writer.(http.Flusher)
	status := http.StatusOK
	errMsg := ""

	var usage *model.Usage
	retryCount := 0
	var lastErr error
	succeeded := false
	for i, m := range dec.Models {
		dec.ServedModel = m.Name
		prov, perr := a.Store.GetProvider(m.ProviderID)
		if perr != nil {
			lastErr = fmt.Errorf("provider not found for model %s", m.Name)
			continue
		}
		apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)
		req.Model = m.Name
		var body map[string]any
		if prov.Protocol == "claude" {
			body, _ = claude.BuildUpstreamRequest(req)
		} else {
			body, _ = openai.BuildUpstreamRequest(req)
		}
		var enc chunkEncoder
		if req.ClientFmt == "claude" {
			enc = &claudeChunkEncoder{enc: claude.NewStreamEncoder(m.Name)}
		} else {
			enc = openaiChunkEncoder{}
		}
		started := false
		rc, streamErr := a.Dispatcher.CallStreamWithRetry(prov.BaseURL, apiKey, prov.Protocol, prov.ProxyURL, body, prov.RetryMax, prov.RetryBackoffMs, func(ch *model.Chunk) error {
			started = true
			if ch != nil && ch.Usage != nil {
				usage = ch.Usage
			}
			if ch == nil {
				c.Writer.Write(enc.Finish())
				flusher.Flush()
				return nil
			}
			c.Writer.Write(enc.EncodeChunk(ch))
			flusher.Flush()
			return nil
		})
		retryCount = rc
		if streamErr == nil {
			dec.FailoverCount = i
			succeeded = true
			break
		}
		lastErr = streamErr
		if started {
			// Output already started; cannot fail over without duplicating content.
			break
		}
		// Pre-first-byte failure: try the next model in the chain.
	}
	if !succeeded && lastErr != nil {
		status = http.StatusBadGateway
		errMsg = lastErr.Error()
		writeGatewayError(c, status, req.ClientFmt, lastErr.Error(), "upstream_error")
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount, usage)
}

func (a *App) writeLog(req *model.ChatRequest, dec *routing.Decision, requestedModel string, status int, dur time.Duration, errMsg string, retryCount int, usage *model.Usage) {
	var prompt, completion, total int
	if usage != nil {
		prompt = usage.PromptTokens
		completion = usage.CompletionTokens
		total = usage.TotalTokens
	}
	var jPrompt, jCompletion, jTotal int
	if dec.JudgeUsage != nil {
		jPrompt = dec.JudgeUsage.PromptTokens
		jCompletion = dec.JudgeUsage.CompletionTokens
		jTotal = dec.JudgeUsage.TotalTokens
	}
	_ = a.Store.CreateLog(&store.RequestLog{
		SessionID:             req.SessionID,
		ClientProtocol:        req.ClientFmt,
		RequestedModel:        requestedModel,
		RoutedModel:           dec.ModelName,
		RouteReason:           dec.Reason,
		JudgeRaw:              dec.JudgeRaw,
		Status:                status,
		LatencyMs:             dur.Milliseconds(),
		Error:                 errMsg,
		RetryCount:            retryCount,
		ServedModel:           dec.ServedModel,
		FailoverCount:         dec.FailoverCount,
		PromptTokens:          prompt,
		CompletionTokens:      completion,
		TotalTokens:           total,
		JudgeModel:            dec.JudgeModel,
		JudgeLatencyMs:        dec.JudgeLatency.Milliseconds(),
		JudgePromptTokens:     jPrompt,
		JudgeCompletionTokens: jCompletion,
		JudgeTotalTokens:      jTotal,
	})
}

func (a *App) handleListModels(c *gin.Context) {
	gs, err := a.Store.ListEnabledModelGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	data := []gin.H{}
	for _, g := range gs {
		chain, err := a.Store.GetGroupChain(g.ID)
		if err != nil || len(chain) == 0 {
			continue
		}
		data = append(data, gin.H{"id": g.Name, "object": "model", "owned_by": "auto-router"})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}
