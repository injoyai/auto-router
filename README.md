# Auto Model Router

> 把零散的模型额度聚合成一个稳定入口,再根据任务难度自动分配模型 —— 一份 API Key,既省心又省钱。

## 🤔 为什么用它

如果你有以下烦恼,这个项目就是为你准备的:

- 🎣 **薅了一堆免费/便宜模型,但挨个用太麻烦**:把多个模型丢进一个队列,对外就是一个模型名。请求时按顺序挨个尝试,前一个失败/超额自动切到下一个,零感知切换。
- ⚖️ **简单任务用大模型太贵,复杂任务用小模型又不够用**:智能路由根据当前这一步请求的意图自动分配 —— 任务规划、架构设计、调试分析等交给强推理模型;写代码、改 bug、翻译改写等交给便宜的快速模型。同一项目不同步骤用不同模型,自动切换。

## ✨ 核心功能

### 1. 🧩 模型聚合:多个模型 → 一个对外入口

把多个模型组成一个**队列**,对外只暴露队列名。客户端只需指定一个 `model` 字段,后端按队列内模型顺序依次尝试:

- 🆓 适合**薅来的免费模型**:额度用完自动切下一个,不用人工盯着换
- 🔌 客户端可用 **OpenAI**(`/v1/chat/completions`)或 **Anthropic**(`/v1/messages`)任一协议调用,上游服务商同样两种协议自由组合(如 Anthropic 客户端 → OpenAI 上游)

### 2. 🧠 智能路由:按任务难度自动分配模型

不再需要手动指定用哪个模型。请求到达时,由**判定队列**分析当前这一步的意图,自动选择最合适的队列:

- 🚀 **强推理模型**负责:需求分析、架构设计、调试分析、代码评审、重构简化、长文本推理、专业咨询、复杂分析、创意构思、逻辑推理等
- 🏃 **便宜/快速模型**负责:编码实现、修复 bug、测试编写、文档编写、简单问答、配置部署、内容生成、翻译改写、文档撰写、学习辅导等

判定队列本身也是普通队列:链式失败转移,判定模型挂了自动切下一个。

## 🚀 快速开始

```bash
# 全新 clone 时 web/dist 尚未构建,需先构建前端(否则二进制不含管理界面)
cd web && npm ci && npm run build && cd ..

go build -o auto-router ./cmd/server
./auto-router
```

首次启动会在 `./data/database/auto-router.db` 中生成并打印 gateway token 和 admin token。浏览器打开 `http://localhost:9090`,用 admin token 登录后台。

> 前端构建产物通过 `go:embed` 打进二进制,**改完前端必须重新构建 `web/dist` 再编译后端**。Docker 构建无需关心这一步,镜像内自动完成前端构建。

## 🛠️ 本地开发

```bash
go run ./cmd/server              # 后端
cd web && npm run dev            # 前端热更新(Windows 可直接运行根目录 run-web.bat)
```

Vite dev server 监听 `http://localhost:5173`,已将 `/v1`、`/admin`、`/health` 代理到本地 9090。若前端不经代理直连后端,需设置 `SERVER_DEV=1` 放开 CORS。

## 🐳 Docker

```bash
docker build -t auto-router .
docker run -p 9090:9090 -v $(pwd)/data:/root/data -v $(pwd)/config:/root/config auto-router
```

镜像为三阶段构建(node 构建前端 → Go 编译 → alpine 运行时),单二进制交付。数据持久化挂载 `/root/data`(SQLite 数据库),配置挂载 `/root/config`(可选 `config.yaml`)。环境变量同样可用 `-e` 传入。

构建时拉取 npm 依赖默认走 `http://127.0.0.1:1080` 代理,可用 `--build-arg HTTP_PROXY= --build-arg HTTPS_PROXY=` 覆盖。

`docker-push.sh` 构建时会自动以当前日期生成版本号(`vYYYY.MM.DD`,CI 环境附加 commit 短哈希)并推送镜像;`./docker-push.sh local` 仅本地构建。`go run` 本地运行时版本号显示为 `dev`。

## ⚙️ 配置

支持两种方式,优先级:**环境变量 > 配置文件 > 默认值**。

### 🌍 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SERVER_LISTEN_ADDR` | `:9090` | 监听地址 |
| `SERVER_DEV` | (未设置) | 任意非空值开启开发模式(CORS 放开) |
| `DB_DRIVER` | `sqlite` | 数据库驱动:`sqlite` 或 `mysql` |
| `DB_PATH` | `./data/database/auto-router.db` | SQLite 文件路径(仅 `sqlite` 驱动使用) |
| `DB_DSN` | (空) | MySQL 连接串(仅 `mysql` 驱动使用,需含 `parseTime=true`) |
| `AUTH_PASSWORD` | 自动生成 | 管理后台登录密码(可覆盖) |
| `AUTH_GATEWAY_TOKEN` | 自动生成 | 客户端访问网关的 token(可覆盖) |
| `CONFIG_FILE` | `./config/config.yaml` | 配置文件路径(不存在则忽略) |

### 📄 配置文件(YAML)

可选。默认读取工作目录下的 `./config/config.yaml`,也可用 `CONFIG_FILE` 指定路径。文件不存在时忽略,字段缺失时回退到环境变量/默认值。完整示例见 [config/config.yaml.examplates](config/config.yaml.examplates)。

```yaml
server:
  listen_addr: ":9090"
  dev: false
db:
  driver: sqlite                 # sqlite (默认) 或 mysql
  path: "./data/database/auto-router.db"  # 仅 sqlite 驱动使用
  dsn: ""                        # 仅 mysql 驱动使用,例如:
  # dsn: "root:password@tcp(127.0.0.1:3306)/auto_router?charset=utf8mb4&parseTime=true&loc=Local"
auth:
  password: "your-admin-password"   # 管理后台登录密码
  gateway_token: "your-gateway-token"
```

未显式设置的 token 仍会在首次启动时自动生成并打印到日志。注意:`config.yaml` 可能含 token 明文,已在 `.gitignore` 中忽略。

## 📋 上手四步

1. 🔌 **添加上游模型**:在「供应商」页添加服务商(BaseURL、API Key、协议),再添加模型。每个服务商可独立配置代理地址与重试参数,方便请求海外 API。
2. 📦 **创建模型队列**:在「模型队列」页创建队列,把模型按优先级拖拽进去(支持拖拽排序)。**记得填写能力说明**,帮助判定模型选择合适的队列。
3. 🎯 **配置路由**:在「路由配置」页选择一个**判定队列**(建议用便宜且较聪明的模型)和**默认兜底队列**(判定失败时用)。网关 API Key 也可在此手动编辑或随机生成。
4. 💬 **客户端调用**:以 OpenAI 协议调用 `POST /v1/chat/completions`(Anthropic 客户端走 `POST /v1/messages`),`Authorization: Bearer <gateway token>`。

   - `model` 留空 / `"auto"` / `"route"` → 自动路由(由判定队列选择)
   - `model` 设为队列名 → 显式指定该队列
   - `X-Route-Model` 头 → 强制指定队列(最高优先,绕过判定)

```bash
curl http://localhost:9090/v1/chat/completions \
  -H "Authorization: Bearer <gateway token>" \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "帮我设计一个缓存淘汰策略"}]}'
```

## 🔀 路由模式

| 模式 | 触发 | route_reason |
|------|------|--------------|
| 🎯 指定路由 | `model` 字段为队列名,或 `X-Route-Model` 头 | `override` |
| 🧠 自动路由 | 判定队列选择成功 | `judge` |
| 🪂 兜底 | 判定失败/未配置判定队列,走默认队列 | `judge` |
| 🧪 测试 | 后台「测试模型/服务商」连通性 | `test` |

指定队列名不存在时直接报错,不会静默回退到兜底队列。

## 🔌 API 一览

**网关(客户端调用,Bearer gateway token)**

| 端点 | 说明 |
|------|------|
| `POST /v1/chat/completions` | OpenAI 协议,支持流式/非流式 |
| `POST /v1/messages` | Anthropic 协议入口,错误响应同样按 Anthropic 格式返回 |
| `GET /v1/models` | 返回可用队列名列表(不是模型名) |

**管理后台(`/admin`)**:`POST /admin/login` 用 admin token 换取 JWT 后访问,涵盖服务商、模型、队列、路由配置、日志、统计的增删改查与连通性测试。

**公共**:`GET /health`、`GET /version`(返回编译时注入的版本号,后台侧边栏会显示)。

## 📦 功能特性

- 🧩 **模型聚合**:多个模型组成队列,按序失败转移,额度用完自动切换,支持拖拽排序;流式请求在首字节前仍可失败转移
- 🧠 **智能路由**:根据任务当前阶段的难度自动分配强推理模型或便宜模型,同一项目不同步骤用不同模型;判定队列本身也是普通队列,链式失败转移
- 🔌 **多协议兼容**:客户端可用 OpenAI 或 Anthropic 协议,上游服务商同样支持两种协议,自由组合(如 Anthropic 客户端 → OpenAI 上游)
- 🌐 **服务商代理与重试**:每个服务商可独立配置代理地址和重试参数(网络错误/5xx/429 指数退避重试)
- 📝 **请求日志与链路追踪**:记录每次请求的路由决策、判定理由、执行模型、服务商、Token 用量和耗时;完整尝试链(判定 + 执行,含每次重试)实时写入,请求进行中即可在日志页看到「进行中」状态
- 🔥 **Token 统计**:按模型(含所属服务商)、按服务商聚合 Token 用量,辅助成本分析
- 📈 **用量趋势**:按天查看 Token 消耗与请求量变化,堆叠柱状图支持按「模型队列」或「模型 · 服务商」两种维度分组、颜色区分,并支持总 Token / 输入 / 输出 / 请求数 / 缓存命中等指标切换
- 🧪 **测试模型不计入统计**:模型连通性测试产生的请求仅记录在日志中,不污染 Token 用量统计与用量趋势图
- 🔒 **API Key 安全**:后端加密存储,前端掩码显示,编辑时支持查看明文
- 🔑 **网关 API Key 管理**:路由配置页可手动编辑或点击刷新图标随机生成网关 Key,运行时生效并持久化
- 💾 **多数据库**:内置 SQLite(纯 Go 驱动,免 CGO),通过 `DB_DRIVER=mysql` 可切换 MySQL

## 🧱 技术栈

- 后端:Go 1.25 + Gin + GORM(SQLite / MySQL)
- 前端:React 18 + TypeScript + Ant Design 5 + Vite,构建产物 `go:embed` 嵌入单一二进制
