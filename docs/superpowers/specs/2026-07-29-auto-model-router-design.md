# Auto Model Router 设计文档

- 日期: 2026-07-29
- 状态: 已批准(待实现)
- 技术栈: Go (后端) + React/Vite/Ant Design (前端) + SQLite

## 1. 概述

一个 AI 模型路由网关。发起请求时,先用一个指定的"判定模型"判断任务难度并选择最合适的模型,再用所选模型执行任务。支持 Agent 显式指定模型、模型在响应中指定下一轮模型、以及自动路由三种模式。对外同时兼容 OpenAI 和 Claude(Anthropic)两种 API 格式,前端可添加多个 API 源。

### 1.1 核心目标

- **自动路由**:请求不带指定模型时,由判定模型选择执行模型
- **Agent 指定**:Agent 可通过 `model` 字段或 `X-Route-Model` 头显式指定模型,跳过路由
- **模型回选**:执行模型可在响应中指定下一轮使用的模型(会话级)
- **双协议兼容**:同时兼容 OpenAI 与 Claude API 格式,支持跨协议路由
- **多源管理**:前端可添加、启停、测试多个 API 源
- **流式支持**:支持 SSE 流式响应

### 1.2 非目标(YAGNI)

- 不做多用户体系(单 Token 鉴权)
- 不做计费/配额管理
- 不做负载均衡(留待后续扩展)
- 不做向量数据库/RAG

## 2. 架构

采用"内部规范格式 + 适配器"方案。定义内部规范 `ChatRequest`/`ChatResponse`(以 OpenAI 格式为基底),所有入站请求先转成规范格式,所有出站请求再从规范格式转成上游原生格式。路由层只操作规范格式。

### 2.1 组件总览

```
┌─────────────────────────────────────────────────────────────┐
│                        客户端 / Agent                        │
│        (OpenAI 格式)            (Claude 格式)                 │
└──────────┬──────────────────────┬───────────────────────────┘
   POST /v1/chat/completions  POST /v1/messages
           ▼                      ▼
┌─────────────────────────────────────────────────────────────┐
│  HTTP Server (Gin) + 单 Token 鉴权中间件                      │
└──────────┬──────────────────────────────────────────────────┘
           ▼
┌─────────────────────────────────────────────────────────────┐
│  入站协议适配器  Inbound Adapter                              │
│   openai→canonical   claude→canonical                        │
└──────────┬──────────────────────────────────────────────────┘
           ▼
┌─────────────────────────────────────────────────────────────┐
│  路由引擎 Routing Engine                                      │
│   ① Agent 指定(model 字段 / X-Route-Model 头)               │
│   ② 会话回选(session.next_model)                            │
│   ③ 自动路由(调用判定模型选名)→ 兜底默认模型                  │
└──────────┬──────────────────────────────────────────────────┘
           ▼
┌─────────────────────────────────────────────────────────────┐
│  上游分发器 Dispatcher                                       │
│   canonical→openai 上游   canonical→claude 上游  (支持 SSE)   │
└──────────┬──────────────────────────────────────────────────┘
           ▼
┌─────────────────────────────────────────────────────────────┐
│  响应后处理 Response Post-processor                          │
│   提取 <<next_model:xxx>> 指令 → 写入 session                │
│   出站适配器 canonical→客户端格式                            │
└──────────┬──────────────────────────────────────────────────┘
           ▼
        返回客户端
```

### 2.2 请求生命周期(数据流)

1. 客户端发请求到 `/v1/chat/completions` 或 `/v1/messages`
2. 鉴权中间件校验单 Token
3. **入站适配器**:把请求转为内部规范格式 `ChatRequest`
4. **路由引擎**按优先级决策:
   - ① 请求带 `model` 且匹配已配置模型名(或 `X-Route-Model` 头)→ 直接用,记 `route_reason=override`
   - ② 请求带 `X-Session-Id` 且该会话存有 `next_model` → 用它,记 `reason=session`
   - ③ 否则调用**判定模型**:传入用户最新消息 + 可用模型列表(名+描述),判定模型返回一个模型名,记 `reason=judge`
5. 校验所选模型存在且启用;不存在则回退到 `default_model_id`,记 `reason=fallback`
6. **分发器**:找到模型所属 Provider,把规范请求转成上游原生格式,发起调用(流式则建立 SSE 管道)
7. **响应后处理**:从响应文本中提取 `<<next_model: 模型名>>` 标记(若启用),从可见文本中剥离,写入 `sessions.next_model`
8. **出站适配器**:把规范响应转回客户端请求时的格式(OpenAI/Claude)返回

## 3. 协议适配器

内部规范格式基于 OpenAI chat completions 结构:
- `messages[]{role, content, tool_calls, tool_call_id}`
- `tools[]`
- `system`(可作为首条 system message 或独立字段)

### 3.1 关键转换点

| 维度 | Claude → 规范 | 规范 → Claude |
|------|--------------|--------------|
| system | 顶层 `system` 字段 → 首条 system message | 首条 system message → 顶层 `system` |
| content blocks | `text`/`tool_use`/`tool_result` 块 → content 字符串 + `tool_calls` + tool 角色消息 | 反向拆分 |
| tool_calls | `tool_use` 块 → `tool_calls` 数组 | `tool_calls` → `tool_use` 块 |
| 流式 | Claude SSE `content_block_delta` → 规范 delta | 规范 delta → Claude SSE 事件 |

OpenAI 适配器近似恒等映射(规范 ≈ OpenAI 格式)。

### 3.2 规范格式定义(摘要)

```go
type ChatRequest struct {
    Model     string        // 规范下通常为空或 "auto",由路由填充
    Messages  []Message
    Tools     []Tool
    Stream    bool
    SessionID string        // 来自 X-Session-Id 头
    Override  string        // 来自 X-Route-Model 头或 model 字段
    ClientFmt string        // openai | claude,出站适配用
}

type Message struct {
    Role       string      // system | user | assistant | tool
    Content    string
    ToolCalls  []ToolCall
    ToolCallID string
}
```

## 4. 路由引擎

### 4.1 路由模式与优先级

优先级从高到低:

1. **Agent 指定 (override)**
   - `X-Route-Model: 模型名` 请求头:最高优先,无视请求体。
   - OpenAI 请求体的 `model` 字段:若等于 `"auto"` / `"route"` / 为空 → 走路由;否则视为显式指定。
   - Claude 请求体的 `model` 字段同理。
   - 匹配规则:精确匹配 `models.name`(发给上游的真实 id),`display_name` 作为友好别名也可匹配。匹配不到 → 不视为 override,继续走路由。

2. **会话回选 (session)**
   - 客户端可选地带 `X-Session-Id` 头(无则不启用会话回选)。
   - 上一次响应中模型若输出了 `<<next_model: xxx>>`,后处理把它存入 `sessions` 表,下一次同 session 请求自动采用。

3. **自动路由 (judge)**
   - 调用配置的判定模型选择模型名。
   - 失败/非法时回退到 `default_model_id` (reason=fallback)。

### 4.2 判定模型调用

- **系统 prompt**:`你是一个模型路由器。根据用户任务和可用模型列表,选择最合适的模型。只回复模型名称,不要解释。`
- **用户内容**:可用模型列表(每行 `名称 - 描述`)+ 用户最新消息(截断到 `judge_max_input_chars`,默认 2000 字符)。
- **解析**:去除空白/引号/markdown 围栏,精确匹配已配置模型名;匹配不到 → fallback。
- **超时**:判定模型调用设短超时(如 10s),超时则 fallback。
- **无循环**:路由每个请求只执行一次。判定模型自身也可出现在候选列表中(它可能适合简单任务);若判定模型选中自己,则直接用它执行,不会再次触发路由。

### 4.3 模型回选指令

- 指令格式:`<<next_model: 模型名>>`(可出现在响应文本任意位置,建议末尾)。
- 当 `enable_next_model_directive=true` 且请求带 `X-Session-Id` 时,在 system prompt 追加注入:`你可以在回复中用 <<next_model: 模型名>> 指定下一轮应使用的模型,该标记不会展示给用户。`
- 响应后处理用正则 `<<next_model:\s*([^>]+)>>` 提取,从可见文本中剥离,写入 `sessions.next_model`。
- 校验模型名存在;不存在则忽略(不回退,因为这是模型的主观建议)。

## 5. 数据模型(SQLite)

```sql
-- API 源(Provider):一个上游端点,带协议类型
CREATE TABLE providers (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  base_url    TEXT NOT NULL,
  api_key     TEXT NOT NULL,               -- AES-GCM 加密存储
  protocol    TEXT NOT NULL,               -- openai | claude
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 模型:挂在某个 Provider 下
CREATE TABLE models (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL,             -- 发给上游的真实 model id
  display_name  TEXT NOT NULL,
  provider_id   INTEGER NOT NULL REFERENCES providers(id),
  description   TEXT,                      -- 给判定模型看的描述
  enabled       INTEGER NOT NULL DEFAULT 1,
  is_judge      INTEGER NOT NULL DEFAULT 0,-- 全局仅一个判定模型
  created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 路由配置(单行,id 固定为 1)
CREATE TABLE routing_config (
  id                            INTEGER PRIMARY KEY CHECK (id = 1),
  judge_model_id                INTEGER REFERENCES models(id),
  default_model_id              INTEGER REFERENCES models(id),
  enable_next_model_directive   INTEGER NOT NULL DEFAULT 1,
  session_ttl_seconds           INTEGER NOT NULL DEFAULT 1800,
  judge_max_input_chars         INTEGER NOT NULL DEFAULT 2000
);

-- 会话(下一轮模型回选)
CREATE TABLE sessions (
  id          TEXT PRIMARY KEY,            -- X-Session-Id
  next_model  TEXT,
  updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  expires_at  DATETIME
);

-- 请求日志
CREATE TABLE request_logs (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id        TEXT,
  client_protocol   TEXT,                  -- openai | claude
  requested_model   TEXT,                  -- 客户端原始 model 字段
  routed_model      TEXT,                  -- 实际选用
  route_reason      TEXT,                  -- override | session | judge | fallback
  judge_raw         TEXT,                  -- 判定模型原始返回
  status            INTEGER,
  latency_ms        INTEGER,
  error             TEXT,
  created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 管理后台与杂项设置(KV)
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT
);
-- 存储: admin_token_hash, judge_system_prompt, encryption_seed 等
```

## 6. API 接口

### 6.1 网关接口(客户端用)

- `POST /v1/chat/completions` — OpenAI 格式
- `POST /v1/messages` — Claude 格式
- `GET /v1/models` — 返回已启用模型列表(OpenAI 风格 `{data:[{id,...}]}`)

请求头:
- `Authorization: Bearer <单Token>`(必需)
- `X-Session-Id: <会话标识>`(可选,启用会话回选)
- `X-Route-Model: <模型名>`(可选,显式指定)

### 6.2 管理接口(前端用,需 admin JWT)

- `POST /admin/login` — 用 admin token 换 JWT
- `GET/POST/PUT/DELETE /admin/providers` — API 源 CRUD
- `POST /admin/providers/:id/test` — 测试连通性
- `GET/POST/PUT/DELETE /admin/models` — 模型 CRUD + 切换 is_judge
- `GET/PUT /admin/routing` — 读取/更新路由配置
- `GET /admin/logs?page=&reason=&model=` — 分页+过滤
- `GET /admin/stats` — 仪表盘计数

## 7. 前端(React + Vite + Ant Design)

页面:
- **Dashboard**:请求数、各模型使用占比、路由原因分布(简单图表)
- **API 源管理**:增删改、测试连通、启停
- **模型管理**:列表、编辑描述、设为判定模型、启停
- **路由配置**:选择判定模型、默认兜底模型、开关 next_model 指令、会话 TTL、判定输入截断长度
- **请求日志**:表格 + 过滤(路由原因/模型/状态)、查看判定模型原始返回

技术栈:React Router、TanStack Query、Ant Design、Axios。构建产物通过 Go `embed` 嵌入二进制,单文件部署。

## 8. 错误处理

- **上游错误**:按客户端协议格式返回错误体
  - OpenAI: `{"error":{"message":"...","type":"...","code":"..."}}`
  - Claude: `{"type":"error","error":{"type":"...","message":"..."}}`
- **判定模型超时/非法返回**:回退 `default_model_id`,日志记 `judge_raw` + warning
- **未配置判定模型且无 override**:回退 `default_model_id`
- **会话过期清理**:后台 goroutine 定期删除 `expires_at < now` 的记录
- **API key 加密**:AES-GCM,密钥派生自部署时生成的随机种子(存于 settings)

## 9. 目录结构

```
auto-router/
├── cmd/router/main.go              # 入口
├── internal/
│   ├── server/                     # HTTP server + 中间件(鉴权/CORS)
│   ├── adapter/                    # 协议适配器
│   │   ├── openai/                 # inbound + outbound
│   │   └── claude/                 # inbound + outbound
│   ├── routing/                    # 路由引擎 + 判定模型调用
│   ├── upstream/                   # 上游分发器(含 SSE 透传)
│   ├── store/                      # SQLite 持久层
│   ├── model/                      # 规范格式 + 数据结构
│   └── config/                     # 配置加载(环境变量 + DB)
├── web/                            # React 前端
│   ├── src/
│   └── dist/                       # 构建产物,被 Go embed
├── go.mod
└── README.md
```

## 10. 测试策略

- **适配器单测**:用 OpenAI/Claude 真实样本 fixture 做双向转换黄金测试
- **路由引擎单测**:mock 判定模型,覆盖 override / session / judge / fallback 四条路径
- **会话回选**:覆盖指令提取与剥离、session 写入与过期
- **集成测试**:httptest 起假上游,端到端跑通 OpenAI 客户端 → Claude 上游的跨协议链路

## 11. 关键决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 协议方案 | 内部规范格式 + 适配器 | 支持跨协议路由,层清晰可测,one-api/new-api 验证过的模式 |
| 路由判定 | 判定模型直接返回模型名 | 用户明确要求;简单直接 |
| 模型回选 | 下一轮切换(会话级) | 适合多轮/Agent 循环,实现清晰 |
| 存储 | SQLite | 单文件部署,零配置,后续可迁移 |
| 鉴权 | 单 Token | 个人/小团队使用,简化范围 |
| 前端 | React + Vite + Ant Design | 生态成熟,组件丰富 |
