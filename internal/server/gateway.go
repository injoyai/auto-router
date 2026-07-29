package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/adapter/claude"
	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
	"auto-router/internal/routing"
	"auto-router/internal/store"
)

// directiveOpen is the opening marker of the <<next_model: ...>> directive,
// used by the streaming path's buffer-detect logic to hold back content that
// might be part of a directive so it is never leaked to the client.
const directiveOpen = "<<next_model:"

// directiveHoldback returns the length of the longest suffix of s that is also
// a prefix of directiveOpen. This is the number of trailing bytes that must be
// held back because they could be the start of a directive completed by a
// later chunk.
func directiveHoldback(s string) int {
	maxK := len(s)
	if maxK > len(directiveOpen) {
		maxK = len(directiveOpen)
	}
	for k := maxK; k > 0; k-- {
		if strings.HasSuffix(s, directiveOpen[:k]) {
			return k
		}
	}
	return 0
}

// nextModelDirectiveInjection is appended to the system prompt when
// enable_next_model_directive is on and the request carries X-Session-Id
// (spec §4.3).
const nextModelDirectiveInjection = "你可以在回复中用 <<next_model: 模型名>> 指定下一轮应使用的模型,该标记不会展示给用户。"

// injectNextModelDirective appends the next-model directive instruction to the
// system prompt. If no system message exists, a new one is inserted at index 0.
func injectNextModelDirective(req *model.ChatRequest) {
	for i := range req.Messages {
		if req.Messages[i].Role == "system" {
			req.Messages[i].Content += "\n" + nextModelDirectiveInjection
			return
		}
	}
	req.Messages = append([]model.Message{{Role: "system", Content: nextModelDirectiveInjection}}, req.Messages...)
}

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

	rc, _ := a.Store.GetRoutingConfig()
	directiveEnabled := rc != nil && rc.EnableNextModelDirective && req.SessionID != ""
	if directiveEnabled {
		injectNextModelDirective(req)
	}

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
		a.streamResponse(c, prov.BaseURL, apiKey, prov.Protocol, body, dec, req, requestedModel, directiveEnabled, start)
		return
	}
	resp, err := a.Dispatcher.Call(prov.BaseURL, apiKey, prov.Protocol, body)
	if err != nil {
		status = http.StatusBadGateway
		errMsg = err.Error()
		writeGatewayError(c, status, clientFmt, err.Error(), "upstream_error")
	} else {
		a.postProcessResponse(req, resp, directiveEnabled)
		// Encode response based on CLIENT protocol
		var b []byte
		if req.ClientFmt == "claude" {
			b, _ = claude.EncodeResponseToClient(resp)
		} else {
			b, _ = openai.EncodeResponseToClient(resp)
		}
		c.Data(http.StatusOK, "application/json", b)
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg)
}

func (a *App) handleChatCompletions(c *gin.Context) {
	a.handleChat(c, "openai", openai.ParseRequest)
}

func (a *App) handleMessages(c *gin.Context) {
	a.handleChat(c, "claude", claude.ParseRequest)
}

func (a *App) streamResponse(c *gin.Context, baseURL, apiKey, protocol string, body map[string]any, dec *routing.Decision, req *model.ChatRequest, requestedModel string, directiveEnabled bool, start time.Time) {
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

	var assembled strings.Builder
	flushed := 0
	directiveSeen := false

	flushText := func(text string) {
		if text == "" {
			return
		}
		ch := &model.Chunk{Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: text}}}}
		c.Writer.Write(enc.EncodeChunk(ch))
		flusher.Flush()
	}

	streamErr := a.Dispatcher.CallStream(baseURL, apiKey, protocol, body, func(ch *model.Chunk) error {
		if ch == nil {
			full := assembled.String()
			if directiveEnabled {
				clean, mname := routing.ExtractNextModel(full)
				if mname != "" {
					a.persistNextModel(req, mname)
				}
				if rem := clean[flushed:]; rem != "" {
					flushText(rem)
				}
			} else if rem := full[flushed:]; rem != "" {
				flushText(rem)
			}
			c.Writer.Write(enc.Finish())
			flusher.Flush()
			return nil
		}
		if !directiveEnabled {
			c.Writer.Write(enc.EncodeChunk(ch))
			flusher.Flush()
			return nil
		}
		if len(ch.Choices) > 0 {
			assembled.WriteString(ch.Choices[0].Delta.Content)
		}
		if directiveSeen {
			return nil
		}
		full := assembled.String()
		if idx := strings.Index(full, directiveOpen); idx >= 0 {
			if idx > flushed {
				flushText(full[flushed:idx])
				flushed = idx
			}
			directiveSeen = true
			return nil
		}
		hold := directiveHoldback(full[flushed:])
		safeEnd := len(full) - hold
		if safeEnd > flushed {
			flushText(full[flushed:safeEnd])
			flushed = safeEnd
		}
		return nil
	})
	if streamErr != nil {
		status = http.StatusBadGateway
		errMsg = streamErr.Error()
		writeGatewayError(c, status, req.ClientFmt, streamErr.Error(), "upstream_error")
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg)
}

func (a *App) postProcessResponse(req *model.ChatRequest, resp *model.ChatResponse, directiveEnabled bool) {
	if !directiveEnabled {
		return
	}
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

func (a *App) writeLog(req *model.ChatRequest, dec *routing.Decision, requestedModel string, status int, dur time.Duration, errMsg string) {
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
