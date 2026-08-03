# MEMORY.md - 项目记忆文档

> 本文件记录项目演进过程中的关键信息，避免上下文丢失或协作时反复回溯。

---

## 技术栈

- **后端**: Go 1.25 + Gin + GORM + 多数据库支持 (SQLite 默认 via glebarez/sqlite 纯 Go 驱动; MySQL via gorm.io/driver/mysql)
- **前端**: React 18 + TypeScript + Ant Design 5 + Vite + TanStack Query
- **设计系统**: "Frosted Botanical" - 毛玻璃植物风，定义在 `web/src/global.css`

## 版本号管理

- 版本变量在 `internal/version/version.go` 的 `Version` 字符串，默认值 `"dev"`（`go run` 或无 ldflags 构建时显示）
- 编译时注入: `go build -ldflags "-X auto-router/internal/version.Version=v2026.08.02"`
- `docker-push.sh` 构建时自动生成 `vYYYY.MM.DD` 日期版本号并通过 `--build-arg VERSION=` 传给 Dockerfile
- 后端暴露 `GET /version` 接口返回 `{"version": "..."}`；前端 Layout.tsx 在挂载时 fetch 并显示在侧边栏品牌副标题 (`AI Gateway · v2026.08.02`)

## 项目结构

```
cmd/main.go                 # 入口
internal/
  config/                    # 配置加载(环境变量 > YAML > 默认值)
  jwt/                       # JWT 认证
  model/                     # 规范化请求/响应模型
  adapter/openai/            # OpenAI 协议适配(入站解析 + 出站请求/响应/流)
  adapter/claude/            # Claude 协议适配
  store/                     # GORM 数据层 (Provider, Model, ModelGroup, RequestLog, RoutingConfig, Setting)
                             # dialer.go: Dialer 接口; sqlite_dialer.go / mysql_dialer.go: 驱动实现
  routing/                   # 路由引擎 + 判定队列调用
  upstream/                  # 上游 HTTP 分发(含代理支持)
  server/                    # Gin 路由 + admin API + gateway 处理
web/
  src/api/                   # 前端 API 客户端
  src/pages/                 # 页面组件
  src/components/Layout.tsx  # 侧边栏布局
  src/global.css             # 全局样式 + CSS 变量
  web.go                     # Go embed 嵌入 dist/
```

## 核心数据模型

### Provider (服务商)
- 字段: `Name`, `BaseURL`, `APIKey`(加密存储), `Protocol`(openai/claude), `ProxyURL`, `RetryMax`, `RetryBackoffMs`, `Enabled`
- `APIKeyPlain` (`gorm:"-"`): 仅用于编辑时返回解密密钥，不持久化
- `HasAPIKey`: 表示是否已保存密钥

### Model (模型)
- 字段: `Name`, `ProviderID`, `Enabled`
- **已移除**: `DisplayName` (无用字段，已从前后端清除)；`IsJudge` (判定改为队列化，由 `RoutingConfig.JudgeGroupID` 指向 `ModelGroup`)

### ModelGroup (模型队列)
- 字段: `Name`, `Remark`, `Enabled`
- **已移除**: `DisplayName`, `Description` (替换为更简单的 `Remark`)
- 队列是对外唯一可路由目标，`/v1/models` 返回队列名而非模型名

### ModelGroupItem
- `GroupID`, `ModelID`, `Position` (升序即请求顺序)
- `ReplaceGroupItems` 整体替换，自动去重

### RequestLog
- `ServedModel`: 实际执行的模型名; `RoutedModel`: 路由目标(队列名)
- Token 字段: `PromptTokens`/`CompletionTokens`/`TotalTokens`/`CacheTokens`（缓存命中）；判定调用另有 `JudgePromptTokens`/`JudgeCompletionTokens`/`JudgeTotalTokens`/`JudgeCacheTokens`
- `CacheTokens` 来源: Claude `cache_read_input_tokens`，OpenAI `prompt_tokens_details.cached_tokens`
- `Trace`: JSON 数组，存储完整请求链路（`[]Attempt`），包含判定尝试和执行模型尝试，每次重试独立一条
- `Attempt` 结构体: `Type`("judge"/"")、`Model`、`Provider`、`Success`、`Status`(HTTP状态码)、`Error`、`LatencyMs`
- `LogWithProvider` 结构体用于 ListLogs JOIN 查询返回服务商名
- Token 统计通过子查询关联 models+providers 解析服务商

### RoutingConfig
- `JudgeGroupID`, `DefaultGroupID`, `GatewayToken`
- 单例行 (ID=1)，首次启动自动 seed
- `GatewayToken` 可在路由配置页手动编辑或点击刷新图标随机生成（48 字符 hex，`crypto.getRandomValues`）

## 关键决策

1. **队列是唯一路由目标**: 客户端 `model` 字段指定队列名，不是模型名
2. **API Key 加密**: 后端 AES 加密存储，编辑时通过 `APIKeyPlain` 返回明文，前端 `Input.Password` 掩码显示
3. **服务商代理**: 每个 Provider 独立配置 `ProxyURL`，Dispatcher 按 proxy URL 缓存 HTTP Client
4. **队列成员拖拽排序**: 前端原生 HTML5 拖拽 API，松手后调用 `ReplaceGroupItems` 保存
5. **多数据库支持**: 通过 `Dialer` 接口抽象驱动差异（`internal/store/dialer.go`）。默认 SQLite，通过 `DB_DRIVER=mysql` 切换 MySQL（DSN 由 `DB_DSN` 提供）。`store.Open(dialer, dsn)` 仅保留通用逻辑（AutoMigrate + seed），驱动特定初始化（SQLite PRAGMA、MySQL 连接池）在各 Dialer 实现内。现有 SQL 均为 ANSI 标准，store 层查询代码零改动

## 路由判定 (Judge Queue)

路由判定由"单模型"改为"判定队列"（链式失败转移，方案 A）：

- `RoutingConfig.JudgeGroupID` 指向 `ModelGroup`（旧 `JudgeModelID` 已删除），判定队列本身也是普通队列
- `JudgeClient.Judge(chain, ...)` 签名接收有序 chain，返回 `(raw, servedModel, usage, err)`；逐模型失败转移封装在 `lazyJudge` 内
- `defaultJudgeClient` 降级为内部辅助（不再实现 `JudgeClient` 接口），单模型签名不变
- 构建候选队列时排除判定队列自身（`g.ID == *rc.JudgeGroupID`），避免自路由
- **已删除**: `Model.IsJudge` 字段、`SetJudgeModel`/`GetJudgeModel`/`IsModelReferenced`、`POST /admin/models/:id/judge` 接口
- **迁移**: `migrateLegacyColumns` 在 `store.Open` 中先于 `migrateLegacyJudge` 执行，DropColumn 删除更早重构遗留的 `model_groups.display_name`/`description`、`models.display_name`（GORM AutoMigrate 不删列，NOT NULL 旧列会阻断新 INSERT）；随后 `migrateLegacyJudge` 以旧 `is_judge` 列为首选源迁移为 'judge' 队列并写入 `JudgeGroupID`，再 DropColumn 删除 `is_judge` 与 `judge_model_id`

### 判定输入与候选信息

- **判定输入**: `engine.go` 使用 `req.AllUserMessages()` 拼接**所有** user 消息内容传给判定模型（旧 `LastUserMessage()` 已弃用，因为多轮对话中最后一条可能只是"继续"，无法反映完整意图）
- **已删除截断**: `TruncateUserText` 函数和 `RoutingConfig.JudgeMaxInputChars` 配置项已移除（含前端表单、admin API、数据库字段定义）。旧数据库的 `judge_max_input_chars` 列残留但 GORM 不映射（无害，未做 DropColumn 迁移）
- **候选描述**: `engine.go` 构建候选队列时将 `ModelGroup.Remark` 填充到 `Candidate.Description`，判定模型可据此精准选择队列（仅靠队列名判定会趋同）。前端 Queues.tsx 的 `remark` 字段标签已改为"能力说明"并配提示文案

### 判定提示词阶段划分 (`judge.go` system prompt)

判定模型先判断是否属于软件开发任务，再按阶段分类:

**软件开发任务 - 需要强推理模型(6类)**: 需求分析、架构设计、调试分析、代码评审、重构简化、长文本推理
**软件开发任务 - 普通/快速模型即可(6类)**: 编码实现、修复bug、测试编写、文档编写、简单问答、配置部署
**非软件开发任务 - 需要强推理模型(5类)**: 专业咨询、复杂分析、创意构思、长文理解、逻辑推理
**非软件开发任务 - 普通/快速模型即可(4类)**: 内容生成、简单咨询、文档撰写、学习辅导

判断要点: 看当前这一步而非整体目标；动词判断("设计/分析/评估/排查/构思"为推理类，"实现/编写/修改/补充/翻译"为执行类)；不确定时倾向选强模型兜底

## 踩坑记录

1. **`gorm:"-:migration"` 仍会 INSERT**: 该标签只跳过建列，不跳过写入。JOIN 查询的附加字段必须用独立结构体(如 `LogWithProvider`)，不能加到 GORM model 上
2. **Model.Enabled 默认值陷阱**: `gorm:"default:true"` 导致 Create 时零值 false 被覆盖为 true。测试中需用 `db.Model().Update("enabled", false)` 显式更新
3. **Antd Select `children` 不渲染**: antd 5.x 的 `options` 中 `children` 字段无效，必须用 `optionRender` prop 自定义下拉项
4. **Select 被表格裁剪**: 表格内 Select 需设置 `getPopupContainer={() => document.body}` 避免被 `overflow` 裁剪
5. **SQLite 并发**: 已启用 WAL + busy_timeout=5000ms 防止 "database is locked"
6. **GORM AutoMigrate 不删列**: 从 struct 移除字段后，旧库的对应列仍残留（含 NOT NULL 约束），会阻断使用新 struct 的 INSERT。`migrateLegacyColumns` 在 `store.Open` 中统一 DropColumn 已知遗留列（`display_name`/`description`）
7. **useEffect 依赖竞态**: `Queues.tsx` 加载队列成员时，effect 依赖只有 `[groups]` 但内部用了从 `models` 派生的 `enabledModels`。`groups` 先于 `models` 加载时 `enabledModels` 为空，所有 model_id 被 filter 掉且不可恢复（空数组 truthy 导致 guard 跳过重载）。修复：加 `!models` 前置判断 + `models` 入 deps
8. **Antd Form.Item 多个子元素导致 setFieldsValue 失效**: `Form.Item` 内若同时放 `<Input>` 和 `<div>` 提示文本，Field 组件收到的是数组而非单个元素，`value` 无法注入到 Input，`form.setFieldsValue` 设置的值不会回显。修复: 提示文本移到 `Form.Item` 的 `extra` 属性，保证 `Form.Item` 只有一个子元素
9. **Modal destroyOnClose 与 form.setFieldsValue 时机冲突**: `Modal` 用 `destroyOnClose` 时，Form 组件在关闭后被销毁，`useEffect` 内的 `setFieldsValue` 可能执行在 Form 重建前，导致编辑时数据为空。修复: 移除 `destroyOnClose`，改在 `openEdit`/`openCreate` 事件处理函数中直接 `form.resetFields()` + `form.setFieldsValue()`

## 后端 API 变更记录

- `GET /version`: 返回 `{"version": "..."}`，版本号由编译时 ldflags 注入
- `DELETE /admin/logs`: 清空所有请求日志（`store.ClearLogs()` 用 `DELETE FROM request_logs WHERE 1=1`，SQLite/MySQL 通用）
- 已删除: `POST /admin/models/:id/judge`、`RoutingConfig.JudgeMaxInputChars` 相关字段/接口

## 前端规范

- **菜单命名**: 「上游模型」(原「模型管理」，Layout.tsx)；页面标题 Sources.tsx 内仍为「模型管理」
- **退出登录按钮**: 位于侧边栏底部，与分隔线之间 6px 间距；分隔线为渐变样式(两端淡中间实)
- **Token 数量显示**: 自动切换单位 (<1K 显示原值, 1K~1M 显示 x.xk, 1M~1B 显示 x.xM, ≥1B 显示 x.xB)，覆盖 Dashboard 统计卡片、饼图、Token 统计页
- **Queues.tsx 表单提示**: 名称和能力说明的提示文本通过 `Form.Item` 的 `extra` 属性显示(灰色 12px)，不使用 tooltip
- **Logs.tsx 列顺序**: 请求模型 -> 服务商 -> 路由模型；工具栏右侧有「清空日志」按钮(danger 风格，带确认弹窗)
- **Logs.tsx 展开行 Token 显示**: 使用「输入/输出/缓存命中」格式（原「提示/补全/合计」已替换）；主请求和判定调用各显示一组；所有行均可展开

## 构建命令

```bash
# 后端
go build ./...          # 编译
go test ./...           # 测试

# 前端 (在 web/ 目录下)
npm run build           # tsc + vite build -> dist/

# 前端改完必须重新编译，dist/ 通过 web.go 的 go:embed 嵌入二进制

# Docker (多阶段构建)
docker build -t auto-router .
#   Stage 1: node:22-alpine → npm ci + npm run build → web/dist
#   Stage 2: golang:1.25-alpine → CGO_ENABLED=0 go build (glebarez/sqlite 纯 Go 无需 CGO)
#   Stage 3: alpine:3.21 + ca-certificates + tzdata, VOLUME /app/data /app/config
```

## 前端页面映射

| 路由 | 页面 | 说明 |
|------|------|------|
| `/` | Dashboard | 概览统计 |
| `/sources` | Sources | 服务商 + 模型管理 |
| `/queues` | Queues | 模型队列(含拖拽排序) |
| `/routing` | Routing | 路由配置(判定队列/兜底队列/API Key 可编辑+随机生成) |
| `/tokens` | Tokens | Token 统计 |
| `/logs` | Logs | 请求日志(含服务商列) |
| `/login` | Login | 登录页 |

## CSS 设计变量

定义在 `web/src/global.css` `:root`:
- 主色: `--primary` (#3a6b4d 植物绿)
- 中性色: `--sand-50` ~ `--sand-900` (暖灰)
- 玻璃: `--glass-bg`, `--glass-border`, `--glass-blur`
- 阴影: `--shadow-xs` ~ `--shadow-lg`
- 圆角: `--radius-sm`(10px) ~ `--radius-2xl`(28px)
- 字体: `--font-display` (Bricolage Grotesque), `--font-body` (DM Sans), `--font-mono` (JetBrains Mono)
