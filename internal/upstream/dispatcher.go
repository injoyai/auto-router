package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		log.Printf("[WARN] upstream %s returned %d: %s", req.URL.String(), resp.StatusCode, string(raw))
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return parseUpstreamResponse(m, protocol)
}

// StreamChunk is one parsed chunk; nil sentinel marks [DONE].
type StreamChunk = *model.Chunk

// CallStream performs a streaming upstream request and invokes onChunk for
// each parsed SSE chunk as it arrives (real streaming, not buffered).
func (d *Dispatcher) CallStream(baseURL, apiKey, protocol string, body map[string]any, onChunk func(StreamChunk) error) error {
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
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		log.Printf("[WARN] upstream %s returned %d: %s", req.URL.String(), resp.StatusCode, string(raw))
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		ch, done, err := parseUpstreamSSELine(line, protocol)
		if err != nil {
			return err
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

// TestConnect issues a GET {baseURL}/models to verify connectivity + credentials.
func (d *Dispatcher) TestConnect(baseURL, apiKey, protocol string) (int, error) {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
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
