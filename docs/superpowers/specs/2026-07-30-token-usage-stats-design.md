# Token 消耗统计功能设计

**日期**:2026-07-30
**状态**:已批准,待实现

## 背景与目标

当前 Auto Router 网关记录每条请求的路由信息(模型、状态、耗时、错误),但未采集 token 用量。用户希望:

1. 按模型 / Provider 维度统计 token 消耗
2. 流式和非流式请求都采集
3. Dashboard 展示总览,独立页面展示详细排行

## 数据层

### RequestLog 表扩展

在 `internal/store/logs.go` 的 `RequestLog` 结构新增 3 个字段:

```go
PromptTokens     int  `json:"prompt_tokens"`
CompletionTokens int  `json:"completion_tokens"`
TotalTokens      int  `json:"total_tokens"`
```

GORM AutoMigrate 自动加列,历史日志这 3 个字段为 0,新请求正常填充。无 schema 破坏风险。

`CreateLog` 和 `ListLogs` 无需改动(GORM 自动处理新字段)。

## 采集层

### 非流式(已具备)

`model.ChatResponse.Usage` 已由 adapter 解析填充:
- OpenAI:`usage.prompt_tokens / completion_tokens / total_tokens`
- Claude:`usage.input_tokens / output_tokens`,在 `claude.ParseResponse` 中映射到 `PromptTokens/CompletionTokens`,TotalTokens 为二者之和

`gateway.go` 的 `handleChat` 在非流式分支已有 `resp *model.ChatResponse`,直接取 `resp.Usage` 传给 `writeLog`。

### 流式(新增)

**问题**:OpenAI/Claude 流式 SSE 都在结束时返回 usage,但当前 `model.Chunk` 没有解析它。

#### model.Chunk 扩展

```go
type Chunk struct {
    Model   string        `json:"model"`
    Choices []ChunkChoice `json:"choices"`
    Usage   *Usage        `json:"usage,omitempty"` // 新增,只在最终 chunk 携带
}
```

用指针 `*Usage` 区分「未返回」(nil)与「返回 0」(非 nil)。

#### openai.ParseSSELine

OpenAI 流式最终 chunk 格式:

```json
{"id":"...","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}
```

`model.Chunk` 已有 `Usage *Usage` 字段,`json.Unmarshal` 自动解析,无需改 `ParseSSELine` 逻辑。

#### claude.ParseSSELine

Claude 的 usage 在 `message_delta` 事件:

```
event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}
```

input_tokens 在 `message_start` 事件的 `usage` 字段。需要:
1. `ParseSSELine` 处理 `event:` 行,维护当前事件类型
2. 在 `message_delta` 解析 `usage.output_tokens`
3. 在 `message_start` 解析 `usage.input_tokens`
4. 合并后填充 `Chunk.Usage`

由于 `ParseSSELine` 是无状态单行解析,需要改为有状态:`claude.StreamEncoder` 已有状态,对称地引入 `claude.StreamParser` 维护事件类型和 input_tokens。

#### dispatcher.CallStreamWithRetry

`onChunk` 回调签名不变(仍接收 `*model.Chunk`),handler 在回调中检查 `ch.Usage != nil` 收集 usage。

#### gateway.streamResponse

```go
var usage *model.Usage
// in onChunk:
if ch != nil && ch.Usage != nil {
    usage = ch.Usage
}
// after stream:
a.writeLog(req, dec, requestedModel, status, dur, errMsg, retryCount, usage)
```

`writeLog` 签名增加 `usage *model.Usage` 参数,nil 时 token 字段为 0。

### TestModel 场景

`handleTestModel` 当前只返回 status/body,未解析响应体为 `ChatResponse`。test 日志的 token 字段保持为 0(测试请求的 token 消耗无统计意义,且 TestModel 用 `max_tokens:1` 几乎无输出)。不额外处理。

## API 层

### 扩展 GET /admin/stats

现有返回:`total`, `by_reason`
新增返回:

```json
{
  "total": 1234,
  "by_reason": [...],
  "tokens": {
    "total": 456789,
    "prompt": 123456,
    "completion": 333333
  },
  "by_model": [
    {"model": "gpt-4o", "count": 100, "total_tokens": 23456, "prompt_tokens": 1234, "completion_tokens": 22222}
  ],
  "by_provider": [
    {"provider": "OpenAI", "count": 80, "total_tokens": 20000, "prompt_tokens": 1000, "completion_tokens": 19000}
  ]
}
```

- `tokens`:全表 SUM 聚合
- `by_model`:`SELECT routed_model, count(*), sum(prompt_tokens), sum(completion_tokens), sum(total_tokens) FROM request_logs GROUP BY routed_model ORDER BY sum(total_tokens) DESC LIMIT 10`
- `by_provider`:JOIN `models` + `providers` 表,按 Provider 名称分组聚合,top 10

`by_provider` 关联链:`request_logs.routed_model` -> `models.name` -> `models.provider_id` -> `providers.id` -> `providers.name`。

处理逻辑:
- `routed_model` 为空的行(test-prov 日志)跳过
- `routed_model` 在 `models` 表无匹配的行跳过(模型已删除等情况)
- 同一 Provider 下多模型聚合到一行,Provider 名称取 `providers.name`

### GET /admin/logs

返回值已含 `prompt_tokens/completion_tokens/total_tokens`(GORM 自动序列化),前端直接展示。

## 前端

### Dashboard 页扩展

- 统计卡新增「Token 消耗」:显示 `tokens.total`,用 k 为单位(如 456.8k,< 1000 时显示原值)
- 新增「Token 按模型分布」环图:复用现有 `Pie` 组件,数据源 `by_model`,angleField=`total_tokens`

### 新增「Token 统计」独立页

路由 `/tokens`,Layout 菜单新增项。布局:

1. **顶部 3 个统计卡**:总消耗 / Prompt / Completion(单位 k)
2. **模型排行表**:`模型名 | 请求数 | 总Token | Prompt | Completion | 占比%`
3. **Provider 排行表**:`Provider | 请求数 | 总Token | Prompt | Completion | 占比%`

表格按 `total_tokens` 倒序,占比 = 该行 total / 全表 total。数据源为扩展后的 `getStats()` 返回。

### Logs 页扩展

表格新增 1 列「Tokens」,展示 `total_tokens`,值为 0 时显示 `-`。

## 测试策略

- **adapter 单测**:
  - OpenAI:构造含 usage 的最终 chunk JSON,断言 `Chunk.Usage` 正确解析
  - Claude:构造 `message_start` + `message_delta` SSE 序列,断言 `StreamParser` 合并 input/output tokens 正确
- **store 单测**:写入带 token 的 RequestLog,SQL 查询 `by_model` 聚合结果正确
- **handler 单测**:模拟流式响应,断言日志写入的 token 值来自最后 chunk
- 复用现有 `apptest_test.go` 的 mock dispatcher 模式

## 边界处理

- 上游不返回 usage(部分自建网关):字段为 0,不报错
- 测试请求(`route_reason=test`):TestModel 响应体含 usage 时解析写入
- 重试场景:只记录最终成功响应的 usage(失败 chunk 无 usage)
- 历史日志:token 字段为 0,聚合 SUM 不受影响

## 不做的事(YAGNI)

- 不做按时间趋势统计(本次明确不选)
- 不做独立 token_usage 表(复用 RequestLog)
- 不做 token 成本估算(需价格表,超出范围)
- 不做历史数据回填(无法获取)
