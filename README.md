# Auto Model Router

AI 模型路由网关。请求到达时,由"判定队列"选择最合适的模型队列执行,支持 Agent 显式指定。对外兼容 OpenAI 协议。

## 核心概念

- **服务商 (Provider)**:API 源,包含 Base URL、API Key、协议(OpenAI/Claude)、代理地址等配置。
- **模型 (Model)**:隶属于某个服务商,是实际执行请求的单元。
- **模型队列 (Queue)**:对外唯一可路由的目标,由一组有序模型组成。请求按队列内模型顺序依次尝试,失败自动转移。客户端在 `model` 字段中指定的是**队列名**。

## 快速开始

```bash
go build -o auto-router ./cmd
./auto-router
```

首次启动会在 `./data/database/auto-router.db` 中生成并打印 gateway token 和 admin token。

## 配置

支持两种方式,优先级:**环境变量 > 配置文件 > 默认值**。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SERVER_LISTEN_ADDR` | `:8080` | 监听地址 |
| `SERVER_DEV` | (未设置) | 任意非空值开启开发模式(CORS 放开) |
| `DB_DRIVER` | `sqlite` | 数据库驱动：`sqlite` 或 `mysql` |
| `DB_PATH` | `./data/database/auto-router.db` | SQLite 文件路径（仅 `sqlite` 驱动使用） |
| `DB_DSN` | (空) | MySQL 连接串（仅 `mysql` 驱动使用，需含 `parseTime=true`） |
| `AUTH_PASSWORD` | 自动生成 | 管理后台登录密码(可覆盖) |
| `AUTH_GATEWAY_TOKEN` | 自动生成 | 客户端访问网关的 token(可覆盖) |
| `CONFIG_FILE` | `./config/config.yaml` | 配置文件路径(不存在则忽略) |

### 配置文件(YAML)

可选。默认读取工作目录下的 `./config/config.yaml`,也可用 `CONFIG_FILE` 指定路径。文件不存在时忽略,字段缺失时回退到环境变量/默认值。

```yaml
server:
  listen_addr: ":8080"
  dev: false
db:
  driver: sqlite                 # sqlite (默认) 或 mysql
  path: "./data/database/auto-router.db"  # 仅 sqlite 驱动使用
  dsn: ""                        # 仅 mysql 驱动使用，例如：
  # dsn: "root:password@tcp(127.0.0.1:3306)/auto_router?charset=utf8mb4&parseTime=true&loc=Local"
auth:
  password: "your-admin-password"   # 管理后台登录密码
  gateway_token: "your-gateway-token"
```

未显式设置的 token 仍会在首次启动时自动生成并打印到日志。注意:`config.yaml` 可能含 token 明文,已在 `.gitignore` 中忽略。

## 使用

1. 用 admin token 登录 `/admin/login`,添加服务商(Provider)和模型。
2. 创建模型队列(Queue),将模型按优先级添加到队列中(支持拖拽排序)。
3. 创建一个判定队列(含用于判定的模型,建议为非推理模型),在路由配置中选择该判定队列,并设置默认兜底队列(`PUT /admin/routing`)。
4. 客户端以 OpenAI 协议调用 `POST /v1/chat/completions`,`Authorization: Bearer <gateway token>`。
   - `model` 留空 / `"auto"` / `"route"` -> 自动路由(由判定队列选择)
   - `model` 设为队列名 -> 显式指定队列
   - `X-Route-Model` 头 -> 强制指定队列(最高优先)

## 路由模式

| 模式 | 触发 | reason |
|------|------|--------|
| Agent 指定 | `model` 字段或 `X-Route-Model` 头 | override |
| 自动路由 | 判定队列选择 | judge |
| 兜底 | 判定失败 | fallback |

## 功能特性

- **模型队列**:聚合多个模型为具名队列,按序失败转移;队列内模型支持拖拽排序
- **智能路由**:由判定队列根据用户任务自动选择最合适的队列
- **多协议兼容**:同时支持 OpenAI 和 Claude 协议接入
- **服务商代理**:每个服务商可独立配置代理地址,方便请求海外 API
- **请求日志**:记录每次请求的路由决策、执行模型、服务商、Token 用量和耗时
- **Token 统计**:按模型、按服务商聚合 Token 用量,辅助成本分析
- **API Key 安全**:后端加密存储,前端掩码显示,编辑时支持查看明文
