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
- `docker-push.sh` 构建时自动生成 `vYYYY.MM.DD` 日期版本号（CI 环境附加 commit 短哈希）并通过 `--build-arg VERSION=` 传给 Dockerfile。支持多架构(`linux/amd64,linux/arm64`) + 推送(`--push`)；用 `IMAGE_NAME` 环境变量指定仓库地址，`./docker-push.sh local` 可仅本地构建
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
- `ServedModel`: 实际执行的模型名; `ServedProvider`: 实际执行的服务商名（请求时记录，不依赖 JOIN 猜测）; `RoutedModel`: 路由目标(队列名)
- Token 字段: `PromptTokens`/`CompletionTokens`/`TotalTokens`/`CacheTokens`（缓存命中）；判定调用另有 `JudgePromptTokens`/`JudgeCompletionTokens`/`JudgeTotalTokens`/`JudgeCacheTokens`
- `CacheTokens` 来源: Claude `cache_read_input_tokens`，OpenAI `prompt_tokens_details.cached_tokens`
- `Trace`: JSON 数组，存储完整请求链路（`[]Attempt`），包含判定尝试和执行模型尝试，每次重试独立一条
- `Attempt` 结构体: `Type`("judge"/"")、`Model`、`Provider`、`Success`、`Status`(HTTP状态码)、`Error`、`LatencyMs`
- `LogWithProvider` 结构体用于 ListLogs 返回服务商名
- Token 统计: `TokenStatsByModel` 按 (model, provider) 分组，`TokenStatsByProvider` 按 provider 分组；均优先使用 `served_provider`（准确），旧日志回退到 models+providers 子查询
- **同名模型多服务商问题**: 同一模型名可能存在于多个服务商下（如 GLM-5.2 同时在智谱和 opencode），MySQL 大小写不敏感导致 `GLM-5.2` = `glm-5.2`。不能通过 JOIN models 表确定实际服务商（会导致重复计数或随机选择）。解决方案：在 `RequestLog` 中直接存储 `served_provider`，由 gateway 在请求执行时填充
- **实时日志**: `Status=0` 表示进行中。路由完成后立即 `CreateLog(status=0)`，每次 attempt(判定+执行)通过 `UpdateLogTrace` 更新 Trace + `served_model` + `served_provider` 字段，请求结束时 `UpdateLogFinal` 写入最终状态。前端 Logs.tsx 对 `status==0` 渲染蓝色「进行中」Tag。服务启动时自动清理 `status=0` 的残留记录

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
6. **路由类型**: `override`（指定路由）、`judge`（智能路由，含判定失败后走兜底队列）、`judge_call`（判定调用）、`test`（测试）。旧的 `fallback` 类型已合并到 `judge`
7. **请求链路追踪**: `RequestLog.Trace` 存储完整链路（JSON `[]Attempt`），包含判定尝试（`Type="judge"`）和执行模型尝试（`Type=""`），每次重试独立一条记录。`Attempt` 含 `Status`（HTTP 状态码）、`LatencyMs`、`Error` 等

## 路由判定 (Judge Queue)

路由判定由"单模型"改为"判定队列"（链式失败转移，方案 A）：

- `RoutingConfig.JudgeGroupID` 指向 `ModelGroup`（旧 `JudgeModelID` 已删除），判定队列本身也是普通队列
- `JudgeClient.Judge(chain, ...)` 签名接收有序 chain，返回 `(raw, servedModel, usage, trace, err)`；逐模型失败转移封装在 `lazyJudge` 内
- `defaultJudgeClient` 降级为内部辅助（不再实现 `JudgeClient` 接口），单模型签名接收 `retryMax`/`retryBackoff` 参数，使用 `CallWithRetry` 支持重试，返回 `[]store.Attempt` trace
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

### 判定采样参数与格式约束

- **OpenAI 协议**: `judge_client.go` 中 body 显式设置 `temperature=0`(确定性判定,避免随机性导致格式漂移)。**不要设 `max_tokens`**: 实测 DeepSeek-V4-Flash 正常判定输出 300~600 token,设过小(如 200)会导致部分服务商返回 200 但 content 为空(可能是输出尚未开始就被截断或触发边界条件)。Claude 协议沿用 `max_tokens=100`(Claude 对该参数实现标准,无此问题)。格式约束主要靠 system prompt + user message 末尾提醒,不靠 `max_tokens` 截断
- **System Prompt 强化**: `judge.go` 的 `judgeSystemPrompt` 开头明确"只负责选择队列,不负责执行用户任务",禁止输出工具调用格式(`tool_calls`、XML、`<｜DSML｜>` 等 markup)、禁止模拟终端命令。回复格式要求改为"严格三行,不要包裹在代码块或 XML 中,不要加引号/markdown 修饰"
- **User Message 末尾格式提醒**: `BuildJudgeMessages` 在 user message 末尾追加"请按格式回复三行...不要调用工具"。长输入下 Flash 模型容易"忘记" system 中的格式要求,在末尾(模型注意力最集中位置)重申可显著降低幻觉概率
- **背景**: DeepSeek-V4-Flash 曾把判定输入(用户真实请求文本)误认为自己要执行的任务,输出 `<｜DSML｜tool_calls>` 格式的工具调用幻觉(548 token),完全无视 system prompt 的三行格式要求,最终静默走默认队列

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
10. **判定链路空内容语义**: `Attempt.Success` 只代表 HTTP 调用成功(`err==nil`)，不等同"判定成功"。`lazyJudge` 真正成功条件是 `err==nil && raw!=""`。当 judge 模型返回 200 但 `choices[0].message.content` 为空时，需显式视为判定失败(标记最后一次 attempt `Success=false`+Error 说明、设 `lastErr`)，否则 trace 会出现"Success=true 却继续判定下一个模型"的假象，且全部空内容时兜底错误是裸 `judge queue exhausted` 无原因。修复见 `lazyJudge.Judge` 的 `err==nil && raw==""` 分支，错误文案 `judge model X returned empty content`
11. **判定链路 choices 为空**: 同 #10 的另一条路径。`defaultJudgeClient.Judge` 在 `len(resp.Choices)==0` 时返回 `err="judge returned no choices"`，但 `CallWithRetry` 的 `onAttempt` 已按 HTTP 层 err==nil 记录 `Success=true`。`lazyJudge` 原有的 `err==nil && raw==""` 分支不命中(err != nil)，不纠正 Success 标记，trace 同样出现"Success=true 却继续判定下一个"的假象。修复:将纠正逻辑推广为"只要 `err != nil` 且最后一次 attempt 仍为 `Success=true`，就统一标失败并补 Error"，覆盖 content 为空、choices 为空、以及未来可能的 HTTP 成功但判定语义失败的所有情况。修复见 `lazyJudge.Judge` 两个连续 `if` 块。典型特征:某次判定 200 + 耗时异常短(如 242ms 处理 7K+ token)却标记成功，链路却继续走下一个判定模型
12. **判定请求 max_tokens 过小导致 empty content**: OpenAI 协议判定请求设 `max_tokens=200`(按"三行格式理论上 50~100 token"估算)后,所有判定都返回 `judge model X returned empty content`。实测 DeepSeek-V4-Flash 正常判定输出 300~600 token(带 [任务][理由] 描述),且部分服务商在 `max_tokens` 小于实际输出时返回 200 但 content 完全为空(非标准截断行为,标准 OpenAI 应返回部分内容 + `finish_reason=length`)。教训:不要用 `max_tokens` 来约束判定输出格式,应靠 system prompt + user message 末尾提醒;`max_tokens` 只在确知模型输出上限时设置。修复:移除 OpenAI 协议的 `max_tokens`,保留 `temperature=0`(该参数对所有服务商安全)
13. **路由失败不记录日志**: `handleChat` 在 `Engine.Route` 返回错误时(如 judge queue exhausted),直接 `writeGatewayError` + `return`,不调用 `writeLog`。导致客户端(Trae 等)报 503 但 Logs 页面无任何记录,难以排查。修复:`writeLog` 改为支持 `dec == nil`(逐字段安全取值),路由失败时也调用 `writeLog` 记录 503 + 错误信息。典型现象:"Trae 报错但日志没有发现错误"
14. **同名模型多服务商导致 Token 统计错乱**: 同一模型名(`deepseek-v4-flash`、`GLM-5.2`/`glm-5.2`)存在于多个服务商下时,通过 JOIN `models` 表解析服务商会产生严重问题:(a) `TokenStatsByProvider` 的 JOIN 匹配到多个服务商,每条日志被重复计数(522条变1042条);(b) `TokenStatsByModel` 的子查询 `LIMIT 1` 随机选一个服务商,opencode 永远没被选中。MySQL 大小写不敏感(`utf8mb4_general_ci`)使 `GLM-5.2` = `glm-5.2` 加剧了此问题。修复:在 `RequestLog` 增加 `ServedProvider` 字段,由 gateway 在请求执行时直接填充(此时已知确切服务商),统计查询优先使用该字段,旧日志通过 trace JSON 回填。`TokenStatsByModel` 改为按 (model, provider) 分组,避免歧义
15. **DailyUsageStats 未聚合导致空数据/request_count 恒为 0**: 初版 `DailyUsageStats` SELECT 了原始列(`served_provider`/`served_model`/`prompt_tokens` 等)却只 `Group("date")`，没有 `SUM`/`COUNT`。后果:(a) MySQL `only_full_group_by` 下报 1055 错误，`handleDailyStats` 记 WARN 后返回空数组，图表显示「暂无数据」;(b) 即使 SQLite 不报错，返回的也是 GROUP BY 下任意行的 token 原始值而非每日合计，`request_count` 永远为 0。修复:物化内层子查询解析 `DATE(created_at)`/`served_model`/`served_provider` 并应用过滤，外层 `Table("(?) as t", inner)` 再 `count(*)` + `sum(...)` 按 `date` 聚合。新增 `TestDailyUsageStats` 覆盖无过滤/按 provider/按 model 三种场景
16. **MySQL `DATE(created_at)` 扫描成 time.Time 导致日期变 RFC3339**: GORM 的 MySQL 驱动默认 `parseTime=true`，`DATE(created_at)` 返回的 DATE 类型会被驱动解析成 `time.Time`，再 Scan 到 `DailyUsageRow.Date`/`DailyUsageByModelRow.Date` 的 string 字段时被 `database/sql` 格式化为 RFC3339（如 `2026-08-13T00:00:00Z`），前端柱状图 x 轴因此显示错误格式。修复:抽出 `dailyDateExpr()`——MySQL 用 `DATE_FORMAT(created_at, '%Y-%m-%d')` 强制输出纯字符串，SQLite 用 `date(created_at)`（本就返回 TEXT `YYYY-MM-DD`）。`DailyUsageStats` 与 `DailyUsageStatsByModel` 均改用它

## 后端 API 变更记录

- `GET /version`: 返回 `{"version": "..."}`，版本号由编译时 ldflags 注入
- `DELETE /admin/logs`: 清空所有请求日志（`store.ClearLogs()` 用 `DELETE FROM request_logs WHERE 1=1`，SQLite/MySQL 通用）
- 已删除: `POST /admin/models/:id/judge`、`RoutingConfig.JudgeMaxInputChars` 相关字段/接口
- **实时日志**: `JudgeClient.Judge` 接口增加 `onAttempt func(store.Attempt)` 回调参数;`Engine.Route` 增加 `onJudgeAttempt` 参数。`gateway.go` 移除 `writeLog`，改为 `CreateLog(status=0)` + `UpdateLogTrace`(每次 attempt) + `UpdateLogFinal`(终态)。`store.Open` 启动时清理 `status=0` 残留记录
- **handleStats 错误日志**: `admin.go` 的 `handleStats` 中 `TokenStatsTotal`/`TokenStatsByModel`/`TokenStatsByProvider` 三个聚合查询的错误不再静默丢弃(`_ =`)，改为 `log.Printf("[WARN] stats: ...")` 记录，便于排查统计为空的原因
- **ListLogs 模型名搜索区分大小写**: MySQL 默认 collation(`utf8mb4_general_ci`) 大小写不敏感，`ListLogs` 的模型过滤加 `BINARY` 关键字强制区分(`BINARY request_logs.routed_model = ?`)；SQLite 的 `=` 本身区分大小写，无需处理。通过 `s.DB.Dialector.Name() == "mysql"` 判断驱动
- **用量趋势 API**: `GET /admin/stats/daily?provider=&model=&days=30` 返回按天聚合的 token 用量（`DailyUsageRow` 含 date/request_count/prompt_tokens/completion_tokens/total_tokens/cache_tokens）。store 方法 `DailyUsageStats(provider, model, days)` 先物化子查询（`DATE(created_at)` + 解析 `served_model`/`served_provider` + 过滤），再在 `(?) as t` 外层做 `count(*)` 与 `sum(...)` 聚合，支持 `served_provider` 和模型名过滤
- **用量趋势按模型 API**: `GET /admin/stats/daily/models?provider=&model=&days=30` 返回按 (date, model, provider) 聚合的数据（`DailyUsageByModelRow` 在 `DailyUsageRow` 基础上多 `model` 和 `provider` 字段），供堆叠柱状图使用（tooltip 需显示供应商，同名模型可存在于多供应商下）。store 方法 `DailyUsageStatsByModel(provider, model, days)` 与 `DailyUsageStats` 共享子查询构造，仅外层 `Group("date, model, provider")` 不同

## 前端规范

- **菜单命名**: 「上游模型」(原「模型管理」，Layout.tsx)；页面标题 Sources.tsx 内仍为「模型管理」
- **协议显示名**: 服务商协议仅做显示名映射（`protocolNames = { openai: 'OpenAI', claude: 'Anthropic' }`，Sources.tsx 列表标签、Routing.tsx 请求地址提示文案用该名；新建/编辑供应商的协议下拉选项为硬编码「OpenAI Chat」「Anthropic Message」）；底层存储值仍是小写 `openai`/`claude`，后端判断依赖它，勿改存储值
- **退出登录按钮**: 位于侧边栏底部，与分隔线之间 6px 间距；分隔线为渐变样式(两端淡中间实)
- **Token 数量显示**: 自动切换单位 (<1K 显示原值, 1K~1M 显示 x.xk, 1M~1B 显示 x.xM, ≥1B 显示 x.xB)，覆盖 Dashboard 统计卡片、饼图、Token 统计页
- **Queues.tsx 表单提示**: 名称和能力说明的提示文本通过 `Form.Item` 的 `extra` 属性显示(灰色 12px)，不使用 tooltip
- **Logs.tsx 列顺序**: 时间 -> 请求模型 -> 路由类型 -> 供应商 -> 执行模型 -> 状态 -> 耗时 -> 输入 -> 输出 -> 缓存命中 -> 错误；原「模型队列」列已改名为「执行模型」，并在其前新增「供应商」列(provider_name 字段，由后端 LogWithProvider JOIN providers 表回填)；工具栏右侧有「清空日志」按钮(danger 风格，带确认弹窗)
- **Logs.tsx 展开行 Token 显示**: 使用「输入/输出/缓存命中」格式（原「提示/补全/合计」已替换）；主请求和判定调用各显示一组；所有行均可展开
- **Logs.tsx 模型名搜索框**: 使用 `Input`（非 `Input.Search`），避免自带搜索图标按钮与旁边"查询"按钮重复；回车通过 `onPressEnter` 触发搜索
- **UsageTrend.tsx**: 用量趋势页，堆叠柱状图 `Column`（`@ant-design/charts`）展示每日 token 消耗，每个柱子按 `model+provider` 叠加（`seriesField: 'key'` + `isStack: true`，`key` 由 `modelKey(model, provider)` 生成 `模型 · 供应商`）；tooltip 用 `customContent` 自定义显示模型+供应商+千分位(`formatThousands`，`toLocaleString`)；筛选区服务商下拉 + 模型下拉（模型下拉 `value` 用 `model.id` 避免同名模型重复，label 未选供应商时带 `(供应商名)`，选了供应商只显示该供应商模型）；时间范围(7/14/30/90天)、指标切换(总Token/输入/输出/请求数/缓存命中)；统计卡片走 `/stats/daily`，图表走 `/stats/daily/models`；x 轴日期显示 `YYYY-MM-DD`（后端 `DATE_FORMAT`/`date(created_at)` 返回，`autoRotate`/`autoHide` 防重叠）；前端用 `formatDate` 生成近 `days` 天完整日期序列，并为每个日期×系列生成数据点（无数据的系列补 0 值，即「稠密数据点」），保证无数据的天在 x 轴占据空位、且图例筛选某个系列后空日期也不会消失（若用单一 fallback 占位点会随该系列被筛选而丢失日期位置）；图表卡片另加 `usage-trend-card` 类以禁用 `aurora-chart-card` 的居中光晕伪元素(`.usage-trend-card .ant-card-body::before { display:none }`)，因为该淡绿羽化光晕是为 Dashboard 饼图设计的，柱状图背景下中心会浮现一个绿点

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
| `/usage-trend` | UsageTrend | 用量趋势(每日折线图) |
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
