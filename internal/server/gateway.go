package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

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

	// B1: capture the client's original requested model BEFORE we overwrite
	// req.Model with the routed model name, so the request log records both
	// accurately (RequestedModel="auto"/display_name/explicit, RoutedModel=chosen).
	requestedModel := req.Model

	// B2: load routing config to drive the next-model directive switch and
	// system-prompt injection (spec §4.3). We deliberately ignore errors here
	// and fall back to "directive disabled" — routing already succeeded, so
	// this must not abort the request.
	rc, _ := a.Store.GetRoutingConfig()
	directiveEnabled := rc != nil && rc.EnableNextModelDirective && req.SessionID != ""
	if directiveEnabled {
		injectNextModelDirective(req)
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
		a.streamResponse(c, prov.BaseURL, apiKey, body, dec, req, requestedModel, directiveEnabled, start)
		return
	}
	resp, err := a.Dispatcher.Call(prov.BaseURL, apiKey, body)
	if err != nil {
		status = http.StatusBadGateway
		errMsg = err.Error()
		c.JSON(status, gin.H{"error": gin.H{"message": err.Error(), "type": "upstream_error"}})
	} else {
		// Post-process next-model directive (guarded by the switch)
		a.postProcessResponse(req, resp, directiveEnabled)
		b, _ := openai.EncodeResponseToClient(resp)
		c.Data(http.StatusOK, "application/json", b)
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg)
}

func (a *App) streamResponse(c *gin.Context, baseURL, apiKey string, body map[string]any, dec *routing.Decision, req *model.ChatRequest, requestedModel string, directiveEnabled bool, start time.Time) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, _ := c.Writer.(http.Flusher)
	status := http.StatusOK
	errMsg := ""

	// I3: streaming directive stripping. Behavior (documented per the task's
	// simpler acceptable approach, with a partial-prefix holdback to avoid
	// leaking "<<" at chunk boundaries):
	//   - When the directive switch is OFF, chunks are flushed verbatim as they
	//     arrive (real streaming, no buffering).
	//   - When the switch is ON, delta content is accumulated. We flush the
	//     portion that cannot be part of a directive immediately, but hold back
	//     any tail that could be the start of "<<next_model:" (partial-prefix
	//     holdback). Once "<<next_model:" is seen, no further content is
	//     flushed until end-of-stream, where ExtractNextModel runs on the full
	//     assembled text and the remaining (cleaned) tail is flushed.
	//   - [DONE] is always emitted.
	var assembled strings.Builder
	flushed := 0 // bytes of assembled already flushed (pre-directive only)
	directiveSeen := false

	flushText := func(text string) {
		if text == "" {
			return
		}
		ch := &model.Chunk{Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: text}}}}
		if b, err := openai.EncodeChunk(ch); err == nil {
			c.Writer.Write(b)
			c.Writer.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}

	streamErr := a.Dispatcher.CallStream(baseURL, apiKey, body, func(ch *model.Chunk) error {
		if ch == nil {
			// [DONE] sentinel: flush any held-back remainder (cleaned when the
			// switch is on), then emit [DONE].
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
			c.Writer.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			return nil
		}
		if !directiveEnabled {
			// Switch off: flush each chunk verbatim, no directive processing.
			b, _ := openai.EncodeChunk(ch)
			c.Writer.Write(b)
			c.Writer.Write([]byte("\n\n"))
			flusher.Flush()
			return nil
		}
		// Switch on: buffer-detect the directive in accumulated content.
		if len(ch.Choices) > 0 {
			assembled.WriteString(ch.Choices[0].Delta.Content)
		}
		if directiveSeen {
			return nil
		}
		full := assembled.String()
		if idx := strings.Index(full, directiveOpen); idx >= 0 {
			// Directive begins at idx; flush the unflushed portion before it.
			if idx > flushed {
				flushText(full[flushed:idx])
				flushed = idx
			}
			directiveSeen = true
			return nil
		}
		// Hold back a tail that could be a partial directive prefix.
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
		c.JSON(status, gin.H{"error": gin.H{"message": streamErr.Error(), "type": "upstream_error"}})
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
