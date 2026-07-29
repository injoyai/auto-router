# Auto Model Router

AI 模型路由网关。请求到达时,由"判定模型"选择最合适的模型执行,支持 Agent 指定与会话级模型回选。对外兼容 OpenAI 协议。

## 快速开始

```bash
go build -o auto-router ./cmd/router
./auto-router
```

首次启动会在 `auto-router.db` 中生成并打印 gateway token 和 admin token。

## 配置(环境变量)

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8080` | 监听地址 |
| `DB_PATH` | `auto-router.db` | SQLite 路径 |
| `GATEWAY_TOKEN` | 自动生成 | 客户端访问网关的 token(可覆盖) |
| `ADMIN_TOKEN` | 自动生成 | 管理后台 token(可覆盖) |

## 使用

1. 用 admin token 登录 `/admin/login`,添加 API 源(Provider)和模型。
2. 设置一个模型为判定模型(`POST /admin/models/:id/judge`),设置默认兜底模型(`PUT /admin/routing`)。
3. 客户端以 OpenAI 协议调用 `POST /v1/chat/completions`,`Authorization: Bearer <gateway token>`。
   - `model` 留空 / `"auto"` / `"route"` → 自动路由
   - `model` 设为具体模型名 → 显式指定
   - `X-Session-Id` 头 → 启用会话级模型回选
   - `X-Route-Model` 头 → 强制指定模型(最高优先)

## 路由模式

| 模式 | 触发 | reason |
|------|------|--------|
| Agent 指定 | `model` 字段或 `X-Route-Model` 头 | override |
| 会话回选 | 上轮响应含 `<<next_model: 名称>>` + `X-Session-Id` | session |
| 自动路由 | 判定模型选择 | judge |
| 兜底 | 判定失败 | fallback |
