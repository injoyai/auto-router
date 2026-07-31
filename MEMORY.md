# MEMORY.md - 项目记忆文档

> 本文件记录项目演进过程中的关键信息，避免上下文丢失或协作时反复回溯。

---

## 技术栈

- **后端**: Go 1.25 + Gin + GORM + 多数据库支持 (SQLite 默认 via glebarez/sqlite 纯 Go 驱动; MySQL via gorm.io/driver/mysql)
- **前端**: React 18 + TypeScript + Ant Design 5 + Vite + TanStack Query
- **设计系统**: "Frosted Botanical" - 毛玻璃植物风，定义在 `web/src/global.css`

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
- `LogWithProvider` 结构体用于 ListLogs JOIN 查询返回服务商名
- Token 统计通过子查询关联 models+providers 解析服务商

### RoutingConfig
- `JudgeGroupID`, `DefaultGroupID`, `JudgeMaxInputChars`, `GatewayToken`
- 单例行 (ID=1)，首次启动自动 seed

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
- **迁移**: `migrateLegacyJudge` 在 `store.Open` 中执行，以旧 `is_judge` 列为首选源迁移为 'judge' 队列并写入 `JudgeGroupID`，随后 DropColumn 删除 `is_judge` 与 `judge_model_id` 两列

## 踩坑记录

1. **`gorm:"-:migration"` 仍会 INSERT**: 该标签只跳过建列，不跳过写入。JOIN 查询的附加字段必须用独立结构体(如 `LogWithProvider`)，不能加到 GORM model 上
2. **Model.Enabled 默认值陷阱**: `gorm:"default:true"` 导致 Create 时零值 false 被覆盖为 true。测试中需用 `db.Model().Update("enabled", false)` 显式更新
3. **Antd Select `children` 不渲染**: antd 5.x 的 `options` 中 `children` 字段无效，必须用 `optionRender` prop 自定义下拉项
4. **Select 被表格裁剪**: 表格内 Select 需设置 `getPopupContainer={() => document.body}` 避免被 `overflow` 裁剪
5. **SQLite 并发**: 已启用 WAL + busy_timeout=5000ms 防止 "database is locked"

## 构建命令

```bash
# 后端
go build ./...          # 编译
go test ./...           # 测试

# 前端 (在 web/ 目录下)
npm run build           # tsc + vite build -> dist/

# 前端改完必须重新编译，dist/ 通过 web.go 的 go:embed 嵌入二进制
```

## 前端页面映射

| 路由 | 页面 | 说明 |
|------|------|------|
| `/` | Dashboard | 概览统计 |
| `/sources` | Sources | 服务商 + 模型管理 |
| `/queues` | Queues | 模型队列(含拖拽排序) |
| `/routing` | Routing | 路由配置(判定队列/兜底队列/API Key) |
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
