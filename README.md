# Auto Model Router

AI 模型路由网关。请求到达时,由"判定模型"选择最合适的模型执行,支持 Agent 显式指定。对外兼容 OpenAI 协议。

## 快速开始

```bash
go build -o auto-router ./cmd/router
./auto-router
```

首次启动会在 `./data/database/auto-router.db` 中生成并打印 gateway token 和 admin token。

## 配置

支持两种方式,优先级:**环境变量 > 配置文件 > 默认值**。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8080` | 监听地址 |
| `DB_PATH` | `./data/database/auto-router.db` | SQLite 路径 |
| `GATEWAY_TOKEN` | 自动生成 | 客户端访问网关的 token(可覆盖) |
| `ADMIN_TOKEN` | 自动生成 | 管理后台 token(可覆盖) |
| `CONFIG_FILE` | `./config/config.yaml` | 配置文件路径(不存在则忽略) |
| `DEV` | (未设置) | 任意非空值开启开发模式(CORS 放开) |

### 配置文件(YAML)

可选。默认读取工作目录下的 `./config/config.yaml`,也可用 `CONFIG_FILE` 指定路径。文件不存在时忽略,字段缺失时回退到环境变量/默认值。

```yaml
listen_addr: ":8080"
db_path: "./data/database/auto-router.db"
password: "your-admin-password"   # 管理后台登录密码(也可用 admin_token,二者等价,admin_token 优先)
gateway_token: "your-gateway-token"
dev: false
```

未显式设置的 token 仍会在首次启动时自动生成并打印到日志。注意:`config.yaml` 可能含 token 明文,已在 `.gitignore` 中忽略。

## 使用

1. 用 admin token 登录 `/admin/login`,添加 API 源(Provider)和模型。
2. 设置一个模型为判定模型(`POST /admin/models/:id/judge`),设置默认兜底模型(`PUT /admin/routing`)。
3. 客户端以 OpenAI 协议调用 `POST /v1/chat/completions`,`Authorization: Bearer <gateway token>`。
   - `model` 留空 / `"auto"` / `"route"` → 自动路由
   - `model` 设为具体模型名 -> 显式指定
   - `X-Route-Model` 头 -> 强制指定模型(最高优先)

## 路由模式

| 模式 | 触发 | reason |
|------|------|--------|
| Agent 指定 | `model` 字段或 `X-Route-Model` 头 | override |
| 自动路由 | 判定模型选择 | judge |
| 兜底 | 判定失败 | fallback |
