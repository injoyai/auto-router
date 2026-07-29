package upstream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
func (d *Dispatcher) Call(baseURL, apiKey string, body map[string]any) (*model.ChatResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
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
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(raw))
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return openai.ParseResponse(m)
}

// StreamChunk is one parsed chunk; nil sentinel marks [DONE].
type StreamChunk = *model.Chunk

// CallStream performs a streaming upstream request and invokes onChunk for
// each parsed SSE chunk as it arrives (real streaming, not buffered). onChunk
// is called with a nil chunk to signal the upstream [DONE] sentinel (and is
// also synthesized once if the upstream closes without one). If onChunk
// returns a non-nil error the stream is aborted and that error is returned.
func (d *Dispatcher) CallStream(baseURL, apiKey string, body map[string]any, onChunk func(StreamChunk) error) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upstream %d: %s", resp.StatusCode, string(raw))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		ch, done, err := openai.ParseSSELine(line)
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
	// Upstream ended without an explicit [DONE]; synthesize one so the caller
	// always sees a terminal nil chunk.
	if onChunk != nil {
		if err := onChunk(nil); err != nil {
			return err
		}
	}
	return nil
}

// TestConnect issues a GET {baseURL}/models to verify connectivity + credentials.
// Returns the upstream HTTP status code (and any transport-level error).
func (d *Dispatcher) TestConnect(baseURL, apiKey string) (int, error) {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
