package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"auto-router/internal/adapter/claude"
	"auto-router/internal/adapter/openai"
	"auto-router/internal/model"
)

// upstreamError carries HTTP status for retry decisions.
type upstreamError struct {
	Status  int    // 0 = network error, otherwise HTTP status code
	Message string
}

func (e *upstreamError) Error() string { return e.Message }

// isRetryable reports whether an error is worth retrying.
// Network errors (status 0), HTTP 5xx, and 429 are retryable.
func isRetryable(err error) bool {
	var ue *upstreamError
	if errors.As(err, &ue) {
		return ue.Status == 0 || ue.Status >= 500 || ue.Status == 429
	}
	return true // unknown errors default retryable
}

type Dispatcher struct {
	Client *http.Client
}

func New() *Dispatcher {
	return &Dispatcher{Client: &http.Client{Timeout: 5 * time.Minute}}
}

// Call performs a non-streaming upstream request and returns the parsed canonical response.
func (d *Dispatcher) Call(baseURL, apiKey, protocol string, body map[string]any) (*model.ChatResponse, error) {
	return d.CallCtx(context.Background(), baseURL, apiKey, protocol, body)
}

// CallCtx is the context-aware variant of Call.
func (d *Dispatcher) CallCtx(ctx context.Context, baseURL, apiKey, protocol string, body map[string]any) (*model.ChatResponse, error) {
	return d.callOnce(ctx, baseURL, apiKey, protocol, body)
}

// callOnce performs a single non-streaming upstream request and returns the
// parsed canonical response. Network and HTTP errors are wrapped in
// *upstreamError so callers can make retry decisions.
func (d *Dispatcher) callOnce(ctx context.Context, baseURL, apiKey, protocol string, body map[string]any) (*model.ChatResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	path := upstreamPath(protocol)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, &upstreamError{Status: 0, Message: err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		log.Printf("[WARN] upstream %s returned %d: %s", req.URL.String(), resp.StatusCode, string(raw))
		return nil, &upstreamError{Status: resp.StatusCode, Message: fmt.Sprintf("upstream returned %d", resp.StatusCode)}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return parseUpstreamResponse(m, protocol)
}

// CallWithRetry calls callOnce with retry logic. Returns the response,
// the number of retries performed, and the last error.
func (d *Dispatcher) CallWithRetry(ctx context.Context, baseURL, apiKey, protocol string, body map[string]any, retryMax, backoffMs int) (*model.ChatResponse, int, error) {
	var lastErr error
	retries := 0
	for attempt := 0; attempt <= retryMax; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(backoffMs*(1<<(attempt-1))) * time.Millisecond)
			retries++
		}
		resp, err := d.callOnce(ctx, baseURL, apiKey, protocol, body)
		if err == nil {
			return resp, retries, nil
		}
		if !isRetryable(err) {
			return nil, retries, err
		}
		lastErr = err
	}
	return nil, retries, lastErr
}

// StreamChunk is one parsed chunk; nil sentinel marks [DONE].
type StreamChunk = *model.Chunk

// CallStream performs a streaming upstream request and invokes onChunk for
// each parsed SSE chunk as it arrives (real streaming, not buffered).
func (d *Dispatcher) CallStream(baseURL, apiKey, protocol string, body map[string]any, onChunk func(StreamChunk) error) error {
	return d.callStreamOnce(baseURL, apiKey, protocol, body, onChunk)
}

// callStreamOnce performs a single streaming upstream request and invokes
// onChunk for each parsed SSE chunk as it arrives (real streaming, not
// buffered). Network and HTTP errors are wrapped in *upstreamError so callers
// can make retry decisions. Scanner and onChunk errors are returned as-is.
func (d *Dispatcher) callStreamOnce(baseURL, apiKey, protocol string, body map[string]any, onChunk func(StreamChunk) error) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	path := upstreamPath(protocol)
	req, _ := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return &upstreamError{Status: 0, Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		log.Printf("[WARN] upstream %s returned %d: %s", req.URL.String(), resp.StatusCode, string(raw))
		return &upstreamError{Status: resp.StatusCode, Message: fmt.Sprintf("upstream returned %d", resp.StatusCode)}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 4*1024*1024)
	var claudeParser *claude.StreamParser
	if protocol == "claude" {
		claudeParser = claude.NewStreamParser("")
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		var ch *model.Chunk
		var done bool
		var perr error
		if claudeParser != nil {
			ch, done, perr = claudeParser.Parse(line)
		} else {
			ch, done, perr = parseUpstreamSSELine(line, protocol)
		}
		if perr != nil {
			return perr
		}
		if done {
			if onChunk != nil {
				if err := onChunk(nil); err != nil {
					return err
				}
			}
			return nil
		}
		if ch != nil && onChunk != nil {
			if err := onChunk(ch); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if onChunk != nil {
		if err := onChunk(nil); err != nil {
			return err
		}
	}
	return nil
}

// CallStreamWithRetry calls callStreamOnce with retry logic.
// Retries only BEFORE the first chunk is sent (pre-first-byte). Once output
// has started, errors are returned immediately to avoid duplicate content.
// Returns the number of retries performed and the last error.
func (d *Dispatcher) CallStreamWithRetry(baseURL, apiKey, protocol string, body map[string]any, retryMax, backoffMs int, onChunk func(StreamChunk) error) (int, error) {
	var lastErr error
	retries := 0
	for attempt := 0; attempt <= retryMax; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(backoffMs*(1<<(attempt-1))) * time.Millisecond)
			retries++
		}
		started := false
		err := d.callStreamOnce(baseURL, apiKey, protocol, body, func(ch StreamChunk) error {
			started = true
			return onChunk(ch)
		})
		if err == nil {
			return retries, nil
		}
		// Already started output - cannot retry (would duplicate content)
		if started {
			return retries, err
		}
		// Pre-first-byte error - check if retryable
		if !isRetryable(err) {
			return retries, err
		}
		lastErr = err
	}
	return retries, lastErr
}

// TestConnect issues a GET {baseURL}/models to verify connectivity + credentials.
// Returns the HTTP status, the (truncated) response body, and any transport error.
func (d *Dispatcher) TestConnect(baseURL, apiKey, protocol string) (int, string, error) {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	setUpstreamAuthHeaders(req, apiKey, protocol)
	log.Printf("[INFO] test connectivity: %s %s (protocol=%s)", req.Method, req.URL.String(), protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		log.Printf("[WARN] test connectivity failed: %v", err)
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := truncateResp(string(raw))
	if resp.StatusCode >= 400 {
		log.Printf("[WARN] test connectivity %s returned %d: %s", req.URL.String(), resp.StatusCode, body)
	}
	return resp.StatusCode, body, nil
}

// TestModel sends a minimal chat request to verify the model is usable.
// Returns the HTTP status, the (truncated) response body, and any transport error.
func (d *Dispatcher) TestModel(baseURL, apiKey, protocol, modelName string) (int, string, error) {
	return d.TestModelCtx(context.Background(), baseURL, apiKey, protocol, modelName)
}

// TestModelCtx is the context-aware variant of TestModel.
func (d *Dispatcher) TestModelCtx(ctx context.Context, baseURL, apiKey, protocol, modelName string) (int, string, error) {
	body := map[string]any{
		"model":      modelName,
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"max_tokens": 1,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}
	path := upstreamPath(protocol)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(b))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setUpstreamAuthHeaders(req, apiKey, protocol)
	log.Printf("[INFO] test model: %s %s (protocol=%s, model=%s)", req.Method, req.URL.String(), protocol, modelName)
	resp, err := d.Client.Do(req)
	if err != nil {
		log.Printf("[WARN] test model failed: %v", err)
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	respBody := truncateResp(string(raw))
	if resp.StatusCode >= 400 {
		log.Printf("[WARN] test model %s returned %d: %s", req.URL.String(), resp.StatusCode, respBody)
	}
	return resp.StatusCode, respBody, nil
}

// truncateResp caps a response body for logging/display.
func truncateResp(s string) string {
	const max = 500
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// upstreamPath returns the endpoint path for the given protocol.
func upstreamPath(protocol string) string {
	if protocol == "claude" {
		return "/messages"
	}
	return "/chat/completions"
}

// setUpstreamAuthHeaders sets the correct auth headers for the protocol.
func setUpstreamAuthHeaders(req *http.Request, apiKey, protocol string) {
	if protocol == "claude" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// parseUpstreamResponse dispatches to the correct protocol adapter.
func parseUpstreamResponse(m map[string]any, protocol string) (*model.ChatResponse, error) {
	if protocol == "claude" {
		return claude.ParseResponse(m)
	}
	return openai.ParseResponse(m)
}

// parseUpstreamSSELine dispatches SSE parsing to the correct protocol adapter.
func parseUpstreamSSELine(line, protocol string) (*model.Chunk, bool, error) {
	if protocol == "claude" {
		return claude.ParseSSELine(line)
	}
	return openai.ParseSSELine(line)
}
