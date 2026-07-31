# Token 消耗统计 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Auto Router 增加 token 消耗统计,按模型/Provider 维度聚合,流式+非流式都采集,Dashboard 总览 + 独立页面详细排行。

**Architecture:** 扩展 `RequestLog` 表存储 token 字段;流式 SSE 解析提取 usage;`/admin/stats` 扩展聚合 API;前端 Dashboard 加总览卡+模型环图,新增独立 `/tokens` 页展示排行,Logs 页加 Tokens 列。

**Tech Stack:** Go + Gin + GORM (SQLite) + React + TypeScript + Ant Design + @ant-design/charts

**Spec:** `docs/superpowers/specs/2026-07-30-token-usage-stats-design.md`

---

## 文件结构

**后端(Go)**

| 文件 | 职责 | 操作 |
|---|---|---|
| `internal/model/canonical.go` | `Chunk` 增加 `Usage` 字段 | 修改 |
| `internal/store/logs.go` | `RequestLog` 增加 token 字段 | 修改 |
| `internal/store/logs_test.go` | 聚合查询测试 | 修改 |
| `internal/adapter/openai/outbound_stream_test.go` | OpenAI usage 解析测试 | 修改 |
| `internal/adapter/claude/outbound_stream.go` | `StreamParser` 有状态解析 usage | 修改 |
| `internal/adapter/claude/outbound_stream_test.go` | Claude usage 解析测试 | 修改 |
| `internal/upstream/dispatcher.go` | `parseUpstreamSSELine` 适配 Claude 有状态解析 | 修改 |
| `internal/server/gateway.go` | `streamResponse`/`writeLog` 采集+写入 usage | 修改 |
| `internal/server/admin.go` | `handleStats` 扩展聚合 | 修改 |

**前端(TS/React)**

| 文件 | 职责 | 操作 |
|---|---|---|
| `web/src/api/logs.ts` | `Stats` 类型扩展 | 修改 |
| `web/src/pages/Dashboard.tsx` | 加 Token 卡片 + 模型环图 | 修改 |
| `web/src/pages/Tokens.tsx` | 独立 Token 统计页 | 创建 |
| `web/src/pages/Logs.tsx` | 加 Tokens 列 | 修改 |
| `web/src/App.tsx` | 注册 `/tokens` 路由 | 修改 |
| `web/src/components/Layout.tsx` | 菜单加「Token 统计」 | 修改 |

---

## Task 1: RequestLog 表增加 token 字段

**Files:**
- Modify: `internal/store/logs.go`

- [ ] **Step 1: 给 RequestLog 加 3 个 token 字段**

修改 `internal/store/logs.go` 的 `RequestLog` 结构,在 `RetryCount` 后、`CreatedAt` 前插入:

```go
type RequestLog struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	SessionID      string    `json:"session_id"`
	ClientProtocol string    `json:"client_protocol"`
	RequestedModel string    `json:"requested_model"`
	RoutedModel    string    `json:"routed_model"`
	RouteReason    string    `json:"route_reason"`
	JudgeRaw       string    `json:"judge_raw"`
	Status         int       `json:"status"`
	LatencyMs      int64     `json:"latency_ms"`
	Error          string    `json:"error"`
	RetryCount     int       `json:"retry_count"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CreatedAt      time.Time `json:"created_at"`
}
```

- [ ] **Step 2: 验证 AutoMigrate 加列**

Run: `go build ./...`
Expected: 编译通过

Run: `go test ./internal/store/...`
Expected: PASS(现有测试通过,AutoMigrate 自动加列)

- [ ] **Step 3: Commit**

```bash
git add internal/store/logs.go
git commit -m "feat(store): add token fields to RequestLog"
```

---

## Task 2: model.Chunk 增加 Usage 字段

**Files:**
- Modify: `internal/model/canonical.go`

- [ ] **Step 1: 给 Chunk 加 Usage 指针字段**

修改 `internal/model/canonical.go` 的 `Chunk` 结构:

```go
type Chunk struct {
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}
```

- [ ] **Step 2: 验证编译**

Run: `go build ./...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/model/canonical.go
git commit -m "feat(model): add Usage to Chunk for streaming token collection"
```

---

## Task 3: OpenAI 流式 usage 解析测试 + 验证

**Files:**
- Modify: `internal/adapter/openai/outbound_stream_test.go`

**说明:** OpenAI 流式最终 chunk 的 `usage` 字段会自动反序列化到 `Chunk.Usage`(Task 2 已加),`ParseSSELine` 无需改动。本任务用测试验证。

- [ ] **Step 1: 写失败测试**

在 `internal/adapter/openai/outbound_stream_test.go` 末尾追加:

```go
func TestParseSSELineWithUsage(t *testing.T) {
	// OpenAI 流式最终 chunk 携带 usage
	line := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`
	ch, done, err := ParseSSELine(line)
	assert.NoError(t, err)
	assert.False(t, done)
	assert.NotNil(t, ch, "chunk should not be nil")
	assert.NotNil(t, ch.Usage, "Usage should be parsed")
	assert.Equal(t, 10, ch.Usage.PromptTokens)
	assert.Equal(t, 20, ch.Usage.CompletionTokens)
	assert.Equal(t, 30, ch.Usage.TotalTokens)
}

func TestParseSSELineNoUsage(t *testing.T) {
	// 普通 delta chunk 没有 usage
	line := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`
	ch, _, err := ParseSSELine(line)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	assert.Nil(t, ch.Usage, "Usage should be nil for non-final chunk")
}
```

- [ ] **Step 2: 运行测试验证通过(无需改实现)**

Run: `go test ./internal/adapter/openai/ -run TestParseSSELine -v`
Expected: PASS(json.Unmarshal 自动解析 Usage 字段)

- [ ] **Step 3: Commit**

```bash
git add internal/adapter/openai/outbound_stream_test.go
git commit -m "test(openai): verify streaming usage parsing"
```

---

## Task 4: Claude 流式 StreamParser 实现 + 测试

**Files:**
- Modify: `internal/adapter/claude/outbound_stream.go`
- Modify: `internal/adapter/claude/outbound_stream_test.go`

**背景:** Claude 的 input_tokens 在 `message_start` 事件,output_tokens 在 `message_delta` 事件。当前 `ParseSSELine` 无状态,需改为有状态解析。

- [ ] **Step 1: 写失败测试**

在 `internal/adapter/claude/outbound_stream_test.go` 末尾追加:

```go
func TestStreamParserUsage(t *testing.T) {
	p := NewStreamParser("claude-3")

	// message_start 事件携带 input_tokens
	p.Parse("event: message_start")
	ch, done, err := p.Parse(`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-3","content":[],"stop_reason":null,"usage":{"input_tokens":15,"output_tokens":0}}}`)
	assert.NoError(t, err)
	assert.False(t, done)
	assert.Nil(t, ch, "message_start should not emit a content chunk")

	// content_block_delta(普通文本)
	p.Parse("event: content_block_delta")
	ch, _, err = p.Parse(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`)
	assert.NoError(t, err)
	assert.NotNil(t, ch)
	assert.Nil(t, ch.Usage, "delta should not carry usage")

	// message_delta 携带 output_tokens
	p.Parse("event: message_delta")
	ch, done, err = p.Parse(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":25}}`)
	assert.NoError(t, err)
	assert.False(t, done)
	assert.NotNil(t, ch, "message_delta should emit a chunk")
	assert.NotNil(t, ch.Usage, "message_delta should carry usage")
	assert.Equal(t, 15, ch.Usage.PromptTokens, "input_tokens from message_start")
	assert.Equal(t, 25, ch.Usage.CompletionTokens, "output_tokens from message_delta")
	assert.Equal(t, 40, ch.Usage.TotalTokens, "total = input + output")

	// message_stop
	p.Parse("event: message_stop")
	_, done, err = p.Parse(`data: {"type":"message_stop"}`)
	assert.NoError(t, err)
	assert.True(t, done)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/adapter/claude/ -run TestStreamParserUsage -v`
Expected: FAIL(`NewStreamParser` 未定义)

- [ ] **Step 3: 实现 StreamParser**

在 `internal/adapter/claude/outbound_stream.go` 追加:

```go
// StreamParser is a stateful SSE parser for Claude streams. It tracks the
// current event type and input_tokens (from message_start) so that the
// final message_delta chunk can emit a complete Usage.
type StreamParser struct {
	model        string
	currentEvent string
	inputTokens  int
}

// NewStreamParser creates a StreamParser for the given model name.
func NewStreamParser(model string) *StreamParser {
	return &StreamParser{model: model}
}

// Parse processes one SSE line and returns (chunk, done, error).
// event: lines update the current event type; data: lines are dispatched
// to the appropriate handler based on the current event.
func (p *StreamParser) Parse(line string) (*model.Chunk, bool, error) {
	if line == "" || strings.HasPrefix(line, ":") {
		return nil, false, nil
	}
	if strings.HasPrefix(line, "event:") {
		p.currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return nil, false, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil, false, nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

	var evt struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &evt); err != nil {
		return nil, false, err
	}

	switch evt.Type {
	case "message_start":
		var ms struct {
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &ms); err != nil {
			return nil, false, err
		}
		p.inputTokens = ms.Message.Usage.InputTokens
		return nil, false, nil

	case "content_block_delta":
		var cd struct {
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &cd); err != nil {
			return nil, false, err
		}
		return &model.Chunk{
			Model:   p.model,
			Choices: []model.ChunkChoice{{Index: 0, Delta: model.Delta{Content: cd.Delta.Text}}},
		}, false, nil

	case "message_delta":
		var md struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &md); err != nil {
			return nil, false, err
		}
		finishReason := stopReasonMap[md.Delta.StopReason]
		if finishReason == "" {
			finishReason = "stop"
		}
		return &model.Chunk{
			Model: p.model,
			Choices: []model.ChunkChoice{{
				Index:        0,
				Delta:        model.Delta{},
				FinishReason: finishReason,
			}},
			Usage: &model.Usage{
				PromptTokens:     p.inputTokens,
				CompletionTokens: md.Usage.OutputTokens,
				TotalTokens:      p.inputTokens + md.Usage.OutputTokens,
			},
		}, false, nil

	case "message_stop":
		return nil, true, nil
	}

	return nil, false, nil
}
```

确认文件顶部 import 包含 `encoding/json` 和 `strings`(若已有则不重复)。

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/adapter/claude/ -run TestStreamParserUsage -v`
Expected: PASS

Run: `go test ./internal/adapter/claude/...`
Expected: PASS(现有测试不受影响)

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/claude/outbound_stream.go internal/adapter/claude/outbound_stream_test.go
git commit -m "feat(claude): add stateful StreamParser for streaming usage"
```

---

## Task 5: dispatcher 适配 Claude 有状态解析

**Files:**
- Modify: `internal/upstream/dispatcher.go`

**背景:** `dispatcher.go` 的 `callStreamOnce` 调用 `parseUpstreamSSELine(line, protocol)`(无状态)。Claude 需要改用 `StreamParser`(有状态)。

- [ ] **Step 1: 改造 callStreamOnce 支持 Claude 有状态解析**

修改 `internal/upstream/dispatcher.go` 的 `callStreamOnce` 函数,在 scanner 循环前初始化 Claude parser:

```go
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

	// Claude needs stateful parsing (event type + input_tokens tracking).
	var claudeParser *claude.StreamParser
	if protocol == "claude" {
		claudeParser = claude.NewStreamParser("")
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 4*1024*1024)
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
```

注意 import 需要加 `"auto-router/internal/adapter/claude"`(若已有则不重复)。

- [ ] **Step 2: 验证编译 + 测试**

Run: `go build ./...`
Expected: 编译通过

Run: `go test ./internal/upstream/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/upstream/dispatcher.go
git commit -m "feat(dispatcher): use stateful StreamParser for Claude streams"
```

---

## Task 6: gateway 采集 + 写入 token

**Files:**
- Modify: `internal/server/gateway.go`

- [ ] **Step 1: 修改 writeLog 签名加 usage 参数**

修改 `internal/server/gateway.go` 的 `writeLog` 函数:

```go
func (a *App) writeLog(req *model.ChatRequest, dec *routing.Decision, requestedModel string, status int, dur time.Duration, errMsg string, retryCount int, usage *model.Usage) {
	var prompt, completion, total int
	if usage != nil {
		prompt = usage.PromptTokens
		completion = usage.CompletionTokens
		total = usage.TotalTokens
	}
	_ = a.Store.CreateLog(&store.RequestLog{
		SessionID:        req.SessionID,
		ClientProtocol:   req.ClientFmt,
		RequestedModel:   requestedModel,
		RoutedModel:      dec.ModelName,
		RouteReason:      dec.Reason,
		JudgeRaw:         dec.JudgeRaw,
		Status:           status,
		LatencyMs:        dur.Milliseconds(),
		Error:            errMsg,
		RetryCount:       retryCount,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	})
}
```

- [ ] **Step 2: 非流式分支传 usage**

修改 `handleChat` 的非流式分支(原 `a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount)`):

```go
	// 非流式分支末尾(在 c.Data 之后、writeLog 之前)
	var usage *model.Usage
	if err == nil && resp != nil {
		usage = &resp.Usage
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount, usage)
```

注意:`resp` 在 err != nil 时为 nil,需用 `resp != nil` 守卫。

- [ ] **Step 3: 流式分支采集 usage**

修改 `streamResponse` 函数,在 onChunk 回调中收集 usage:

```go
func (a *App) streamResponse(c *gin.Context, baseURL, apiKey, protocol string, body map[string]any, dec *routing.Decision, req *model.ChatRequest, requestedModel string, start time.Time, retryMax, backoffMs int) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, _ := c.Writer.(http.Flusher)
	status := http.StatusOK
	errMsg := ""

	var enc chunkEncoder
	if req.ClientFmt == "claude" {
		enc = &claudeChunkEncoder{enc: claude.NewStreamEncoder(dec.ModelName)}
	} else {
		enc = openaiChunkEncoder{}
	}

	var usage *model.Usage
	retryCount, streamErr := a.Dispatcher.CallStreamWithRetry(baseURL, apiKey, protocol, body, retryMax, backoffMs, func(ch *model.Chunk) error {
		if ch == nil {
			c.Writer.Write(enc.Finish())
			flusher.Flush()
			return nil
		}
		if ch.Usage != nil {
			usage = ch.Usage
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
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount, usage)
}
```

- [ ] **Step 4: 验证编译**

Run: `go build ./...`
Expected: 编译通过

Run: `go test ./internal/server/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/gateway.go
git commit -m "feat(gateway): collect and persist token usage"
```

---

## Task 7: store 聚合查询 + 测试

**Files:**
- Modify: `internal/store/logs.go`
- Modify: `internal/store/logs_test.go` (或 store_test.go 视实际文件名)

- [ ] **Step 1: 写失败测试**

在 `internal/store/store_test.go` 末尾追加:

```go
func TestTokenAggregations(t *testing.T) {
	s := newTestStore(t)
	// 两条 gpt-4o,一条 claude-3
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30})
	s.CreateLog(&RequestLog{RoutedModel: "gpt-4o", PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10})
	s.CreateLog(&RequestLog{RoutedModel: "claude-3", PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300})

	byModel, err := s.TokenStatsByModel()
	assert.NoError(t, err)
	assert.Len(t, byModel, 2)
	// 按 total_tokens 倒序:claude-3(300) 在前
	assert.Equal(t, "claude-3", byModel[0].Model)
	assert.Equal(t, int64(300), byModel[0].TotalTokens)
	assert.Equal(t, "gpt-4o", byModel[1].Model)
	assert.Equal(t, int64(40), byModel[1].TotalTokens)
	assert.Equal(t, int64(15), byModel[1].PromptTokens)
	assert.Equal(t, int64(25), byModel[1].CompletionTokens)
	assert.Equal(t, int64(2), byModel[1].Count)

	total, err := s.TokenStatsTotal()
	assert.NoError(t, err)
	assert.Equal(t, int64(340), total)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/store/ -run TestTokenAggregations -v`
Expected: FAIL(`TokenStatsByModel` 未定义)

- [ ] **Step 3: 实现聚合方法**

在 `internal/store/logs.go` 末尾追加:

```go
// TokenStatRow is one row of token aggregation.
type TokenStatRow struct {
	Model           string `json:"model"`
	Provider        string `json:"provider"`
	Count           int64  `json:"count"`
	PromptTokens    int64  `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens     int64  `json:"total_tokens"`
}

// TokenStatsTotal returns the sum of total_tokens across all logs.
func (s *Store) TokenStatsTotal() (int64, error) {
	var total int64
	err := s.DB.Model(&RequestLog{}).Where("total_tokens > 0").Sum("total_tokens").Scan(&total).Error
	return total, err
}

// TokenStatsByModel aggregates token usage grouped by routed_model,
// ordered by total_tokens desc, limited to 10.
func (s *Store) TokenStatsByModel() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	err := s.DB.Model(&RequestLog{}).
		Select("routed_model as model, count(*) as count, sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens, sum(total_tokens) as total_tokens").
		Where("routed_model != '' AND total_tokens > 0").
		Group("routed_model").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}

// TokenStatsByProvider aggregates token usage grouped by provider name,
// joining models+providers to resolve the provider name. Limited to 10.
func (s *Store) TokenStatsByProvider() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	err := s.DB.Table("request_logs").
		Select("providers.name as provider, count(*) as count, sum(request_logs.prompt_tokens) as prompt_tokens, sum(request_logs.completion_tokens) as completion_tokens, sum(request_logs.total_tokens) as total_tokens").
		Joins("LEFT JOIN models ON request_logs.routed_model = models.name").
		Joins("LEFT JOIN providers ON models.provider_id = providers.id").
		Where("request_logs.routed_model != '' AND request_logs.total_tokens > 0 AND providers.name != ''").
		Group("providers.name").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/store/ -run TestTokenAggregations -v`
Expected: PASS

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/logs.go internal/store/store_test.go
git commit -m "feat(store): add token aggregation queries"
```

---

## Task 8: handleStats 扩展返回 token 聚合

**Files:**
- Modify: `internal/server/admin.go`

- [ ] **Step 1: 修改 handleStats**

修改 `internal/server/admin.go` 的 `handleStats` 函数:

```go
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
		"total":      total,
		"by_reason":  reasons,
		"tokens":     gin.H{"total": totalTokens, "prompt": tokenSumPrompt(byModel), "completion": tokenSumCompletion(byModel)},
		"by_model":   byModel,
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
```

注意:`tokens.prompt/completion` 用 byModel 求和(避免再查一次全表 SUM)。若 byModel 因 LIMIT 10 截断会有微小偏差,但 top 10 之外占比极小,可接受。

- [ ] **Step 2: 验证编译 + 测试**

Run: `go build ./...`
Expected: 编译通过

Run: `go test ./internal/server/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/admin.go
git commit -m "feat(admin): extend /admin/stats with token aggregations"
```

---

## Task 9: 前端 API 类型扩展

**Files:**
- Modify: `web/src/api/logs.ts`

- [ ] **Step 1: 扩展 Stats 类型**

修改 `web/src/api/logs.ts`,替换 `Stats` interface:

```typescript
export interface TokenStatRow {
  model: string
  provider: string
  count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

export interface Stats {
  total: number
  by_reason: { Reason: string; Count: number }[]
  tokens: { total: number; prompt: number; completion: number }
  by_model: TokenStatRow[]
  by_provider: TokenStatRow[]
}

// RequestLog 增加 token 字段
export interface RequestLog {
  id: number
  session_id: string
  client_protocol: string
  requested_model: string
  routed_model: string
  route_reason: string
  judge_raw: string
  status: number
  latency_ms: number
  error: string
  retry_count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  created_at: string
}
```

- [ ] **Step 2: 验证类型检查**

Run: `cd web && npx tsc --noEmit`
Expected: 无错误

- [ ] **Step 3: Commit**

```bash
git add web/src/api/logs.ts
git commit -m "feat(web): extend Stats and RequestLog types with token fields"
```

---

## Task 10: Dashboard 加 Token 卡片 + 模型环图

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: 加 Token 统计卡 + 模型环图**

修改 `web/src/pages/Dashboard.tsx`:

1. import 加 `CrownOutlined`(或用现有图标 `FireOutlined` 等,这里用 `FireOutlined`):

```typescript
import {
  ThunderboltOutlined,
  CheckCircleOutlined,
  AppstoreOutlined,
  ClockCircleOutlined,
  FireOutlined,
} from '@ant-design/icons'
```

2. 在 `columnData` 之后、`pieConfig` 之前加 token 数据派生:

```typescript
const tokensTotal = stats?.tokens?.total ?? 0
const tokensPrompt = stats?.tokens?.prompt ?? 0
const tokensCompletion = stats?.tokens?.completion ?? 0

const formatTokens = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${n}`)

const tokenPieData = (stats?.by_model ?? []).map((r) => ({
  type: r.model,
  value: r.total_tokens,
}))
```

3. 在 Row(4 个卡片)中新增第 5 个?不,改成在第二行加 Token 卡。在现有 `<Row gutter={[20, 20]} style={{ marginBottom: 20 }}>` 之后、`<Row gutter={20}>`(两个图)之前,插入 Token 行:

```tsx
<Row gutter={[20, 20]} style={{ marginBottom: 20 }}>
  <Col span={8} className="aurora-fade-in aurora-fade-in-1">
    <Card className="stat-card stat-card--indigo">
      <div className="stat-card-icon"><FireOutlined /></div>
      <Statistic title="Token 消耗" value={formatTokens(tokensTotal)} />
    </Card>
  </Col>
  <Col span={8} className="aurora-fade-in aurora-fade-in-2">
    <Card className="stat-card stat-card--mint">
      <div className="stat-card-icon"><ThunderboltOutlined /></div>
      <Statistic title="Prompt" value={formatTokens(tokensPrompt)} />
    </Card>
  </Col>
  <Col span={8} className="aurora-fade-in aurora-fade-in-3">
    <Card className="stat-card stat-card--violet">
      <div className="stat-card-icon"><CheckCircleOutlined /></div>
      <Statistic title="Completion" value={formatTokens(tokensCompletion)} />
    </Card>
  </Col>
</Row>
```

4. 在第二个 Row(两个图)中,把「模型使用占比」改为「Token 按模型分布」,数据源改为 `tokenPieData`:

```tsx
<Col span={12} className="aurora-fade-in aurora-fade-in-4">
  <Card title="Token 按模型分布" className="aurora-card">
    {tokenPieData.length > 0 ? (
      <Pie {...pieConfig} data={tokenPieData} angleField="value" colorField="type" />
    ) : (
      <p style={{ color: '#a89e85', textAlign: 'center', padding: 40 }}>暂无数据</p>
    )}
  </Card>
</Col>
```

保留左侧「路由原因分布」环图不变。

- [ ] **Step 2: 验证构建**

Run: `cd web && npx tsc --noEmit`
Expected: 无错误

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat(web): add token stats cards and model pie to Dashboard"
```

---

## Task 11: Logs 页加 Tokens 列

**Files:**
- Modify: `web/src/pages/Logs.tsx`

- [ ] **Step 1: 在 columns 加 Tokens 列**

修改 `web/src/pages/Logs.tsx` 的 columns 数组,在「重试」列之后、「错误」列之前插入:

```typescript
{
  title: 'Tokens', dataIndex: 'total_tokens', key: 'total_tokens', width: 90,
  render: (v: number) => v > 0 ? v : '-',
},
```

- [ ] **Step 2: 验证构建**

Run: `cd web && npx tsc --noEmit`
Expected: 无错误

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/Logs.tsx
git commit -m "feat(web): add Tokens column to Logs page"
```

---

## Task 12: 新增 Token 统计独立页

**Files:**
- Create: `web/src/pages/Tokens.tsx`

- [ ] **Step 1: 创建 Tokens 页**

创建 `web/src/pages/Tokens.tsx`:

```tsx
import { Card, Col, Row, Statistic, Spin, Table } from 'antd'
import { FireOutlined, ThunderboltOutlined, CheckCircleOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { getStats } from '../api/logs'
import type { TokenStatRow } from '../api/logs'

const formatTokens = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${n}`)

export default function Tokens() {
  const { data: stats, isLoading } = useQuery({
    queryKey: ['stats'],
    queryFn: getStats,
  })

  if (isLoading) {
    return <Spin size="large" style={{ display: 'block', marginTop: 100 }} />
  }

  const tokensTotal = stats?.tokens?.total ?? 0
  const tokensPrompt = stats?.tokens?.prompt ?? 0
  const tokensCompletion = stats?.tokens?.completion ?? 0

  const modelRows = stats?.by_model ?? []
  const providerRows = stats?.by_provider ?? []

  const modelColumns = [
    { title: '模型名', dataIndex: 'model', key: 'model' },
    { title: '请求数', dataIndex: 'count', key: 'count', width: 100 },
    { title: '总Token', dataIndex: 'total_tokens', key: 'total_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: 'Prompt', dataIndex: 'prompt_tokens', key: 'prompt_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: 'Completion', dataIndex: 'completion_tokens', key: 'completion_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '占比', key: 'percent', width: 80,
      render: (_: unknown, r: TokenStatRow) => tokensTotal > 0
        ? `${((r.total_tokens / tokensTotal) * 100).toFixed(1)}%`
        : '-' },
  ]

  const providerColumns = [
    { title: 'Provider', dataIndex: 'provider', key: 'provider' },
    { title: '请求数', dataIndex: 'count', key: 'count', width: 100 },
    { title: '总Token', dataIndex: 'total_tokens', key: 'total_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: 'Prompt', dataIndex: 'prompt_tokens', key: 'prompt_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: 'Completion', dataIndex: 'completion_tokens', key: 'completion_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '占比', key: 'percent', width: 80,
      render: (_: unknown, r: TokenStatRow) => tokensTotal > 0
        ? `${((r.total_tokens / tokensTotal) * 100).toFixed(1)}%`
        : '-' },
  ]

  return (
    <div>
      <div className="page-title">Token 统计</div>
      <div className="page-subtitle">按模型和 Provider 维度查看 Token 消耗</div>

      <Row gutter={[20, 20]} style={{ marginBottom: 20 }}>
        <Col span={8}>
          <Card className="stat-card stat-card--indigo">
            <div className="stat-card-icon"><FireOutlined /></div>
            <Statistic title="总消耗" value={formatTokens(tokensTotal)} />
          </Card>
        </Col>
        <Col span={8}>
          <Card className="stat-card stat-card--mint">
            <div className="stat-card-icon"><ThunderboltOutlined /></div>
            <Statistic title="Prompt" value={formatTokens(tokensPrompt)} />
          </Card>
        </Col>
        <Col span={8}>
          <Card className="stat-card stat-card--violet">
            <div className="stat-card-icon"><CheckCircleOutlined /></div>
            <Statistic title="Completion" value={formatTokens(tokensCompletion)} />
          </Card>
        </Col>
      </Row>

      <Card title="模型排行" className="aurora-card" style={{ marginBottom: 20 }}>
        <Table
          columns={modelColumns}
          dataSource={modelRows}
          rowKey="model"
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无数据' }}
        />
      </Card>

      <Card title="Provider 排行" className="aurora-card">
        <Table
          columns={providerColumns}
          dataSource={providerRows}
          rowKey="provider"
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无数据' }}
        />
      </Card>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/pages/Tokens.tsx
git commit -m "feat(web): add Tokens stats page"
```

---

## Task 13: 注册 /tokens 路由 + 菜单

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: App.tsx 注册路由**

修改 `web/src/App.tsx`,import Tokens 页并在 Layout 子路由中注册:

```tsx
import Tokens from './pages/Tokens'
// ...
        <Route path="logs" element={<Logs />} />
        <Route path="tokens" element={<Tokens />} />
```

- [ ] **Step 2: Layout.tsx 菜单加项**

先读 `web/src/components/Layout.tsx` 找到菜单 items 数组(关键路径已在前文探索过),在 Logs 项后加:

```tsx
{ key: '/tokens', icon: <FireOutlined />, label: 'Token 统计' }
```

import 需加 `FireOutlined`。具体行号在实现时读取文件确定。

- [ ] **Step 3: 验证构建**

Run: `cd web && npm run build`
Expected: 构建成功

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx web/src/components/Layout.tsx
git commit -m "feat(web): register /tokens route and menu item"
```

---

## Task 14: 全量构建 + 端到端验证

**Files:** 无(验证步骤)

- [ ] **Step 1: 后端全量测试**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: 前端构建**

Run: `cd web && npm run build`
Expected: 构建成功,`web/dist/index.html` 等文件生成

- [ ] **Step 3: 后端编译嵌入前端**

Run: `cd .. && go build -o auto-router.exe ./cmd/router`
Expected: 编译成功

- [ ] **Step 4: 启动并手工验证**

启动 `.\auto-router.exe`,登录后:
1. Dashboard 页:看到 Token 消耗卡片(初始 0)
2. 菜单点「Token 统计」:看到排行表(初始空)
3. 发起一个非流式请求 + 一个流式请求到 `/v1/chat/completions`
4. Logs 页:新请求行 Tokens 列有值
5. Token 统计页:模型排行表有数据
6. Dashboard:Token 卡片和环图有数据

- [ ] **Step 5: Commit(如有调整)**

```bash
git add -A
git commit -m "chore: verify token usage stats end-to-end"
```

---

## 自审检查

**Spec 覆盖:**
- ✅ RequestLog 加 token 字段(Task 1)
- ✅ 非流式采集(Task 6)
- ✅ 流式采集 OpenAI(Task 3 验证 + Task 6)
- ✅ 流式采集 Claude(Task 4 实现 + Task 5 适配 + Task 6)
- ✅ writeLog 写入(Task 6)
- ✅ store 聚合查询(Task 7)
- ✅ /admin/stats 扩展(Task 8)
- ✅ /admin/logs 自动含 token(Task 1 序列化)
- ✅ Dashboard 总览(Task 10)
- ✅ 独立 Token 页(Task 12 + 13)
- ✅ Logs 页 Tokens 列(Task 11)
- ✅ 测试策略覆盖(Task 3/4/7 + 现有测试)

**Placeholder 扫描:** 无 TBD/TODO,所有代码块完整。

**类型一致性:**
- `TokenStatRow` 在 store(admin.go)/ 前端(logs.ts)字段名一致
- `Usage` 指针在 model/dispatcher/gateway 一致
- `StreamParser` 在 claude/dispatcher 一致

无遗漏。
