# 模型队列(有序失败转移)设计

**日期：** 2026-07-31
**状态：** 已确认，待实施
**关联：** 基于 `2026-07-29-auto-model-router-design.md` 路由核心与 `2026-07-30-merge-providers-models-design.md` 模型管理

## 1. 背景与动机

当前路由引擎 `Engine.Route()` 只返回**单个** `Model`,网关对其 Provider 发起一次调用,失败后(即使 `CallWithRetry` 重试耗尽)直接返回错误,没有跨模型兜底;且模型可被客户端直接指名调用。

本设计引入"模型队列"概念,并确立一条核心规则:**只有队列(group)是对外可路由目标,模型必须经队列才能对外提供服务**。一个具名队列(如 `deepseek-v4-flash`)映射到一组有序的现有模型,请求命中队列时按顺序尝试,任一模型失败即转向下一个,直到成功或全部失败。智能路由(judge)的候选也只包含队列名;显式 `model=队列名` / `X-Route-Model: 队列名` 同样命中队列。判定模型(judge)本身是内部路由基础设施,仍作为单个模型直接配置,不放进队列。

## 2. 关键决策（已与用户确认）

1. **只有队列可路由**：模型不能被客户端/judge/默认兜底直接使用,必须归属于某个队列;队列是对外唯一路由目标。
2. **数据模型**：队列是命名别名,通过有序关联表引用现有 Model,不改变 Model 本身。
3. **失败转移条件**：单模型重试沿用 `isRetryable`(网络错误/5xx/429 才重试,4xx 立即不重试);**队列层任何错误都转向下一个模型**(4xx 也转移)。
4. **重试叠加**：队列中每个模型先用其 Provider 的 `RetryMax` 重试,全部失败后再转移。
5. **智能路由集成**：judge 候选列表**只含队列名**(不含单个模型名)。
6. **显式非队列名直接报错**：客户端 `model=某名称` 不是任何队列时,直接返回错误(不回退到 judge)。
7. **默认兜底改为默认队列**：`RoutingConfig` 的 `DefaultModelID` 替换为 `DefaultGroupID`。
8. **判定模型仍为单个模型**：judge 不放进队列,直接配置(内部使用,不对外提供生成服务)。

## 3. 数据模型

新增两张表,在 `internal/store/store.go` 的 `AutoMigrate` 中注册。

### `ModelGroup`（队列）

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | uint PK | |
| Name | string, unique | 队列名,如 `deepseek-v4-flash`,作为路由目标名 |
| DisplayName | string | 展示名 |
| Description | string | 给 judge 看的描述 |
| Enabled | bool | 是否启用(禁用的队列不进入 judge 候选/不可路由) |
| CreatedAt | time.Time | |

### `ModelGroupItem`（有序关联）

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | uint PK | |
| GroupID | uint | 外键 -> ModelGroup |
| ModelID | uint | 外键 -> Model |
| Position | int | 顺序(0,1,2...),请求按此升序尝试 |

约束: `(GroupID, ModelID)` 唯一、`(GroupID, Position)` 唯一,防止重复与位置冲突。`Position` 由后端在写入时按数组下标重写,前端无需关心绝对值。

### Store 新增方法

- `ListModelGroups()` / `ListEnabledModelGroups()`
- `GetModelGroup(id)` / `GetModelGroupByName(name)`
- `CreateModelGroup(g)` / `UpdateModelGroup(g)` / `DeleteModelGroup(id)`(级联删除 items)
- `GetGroupItemsOrdered(groupID)` -> `[]ModelGroupItem`(按 Position 升序)
- `ReplaceGroupItems(groupID, modelIDs []uint)` -> 事务内删除旧 items,按 `modelIDs` 下标重写 Position 插入
- `CountGroupsByModel(modelID)`(可选,诊断用)

## 4. 路由引擎改动（`internal/routing`）

### Decision 扩展为携带有序链

```go
type Decision struct {
    ModelName   string         // 目标名(队列名),用于日志 RoutedModel
    Model       *store.Model   // 链首模型(向后兼容,日志/judge 诊断用)
    Models      []*store.Model // 有序链:队列内启用且 Provider 启用的模型,按 Position 升序
    Reason      string         // override | judge | fallback
    ServedModel string         // 实际服务的模型名(由网关成功后回填)
    JudgeRaw    string
    JudgeModel  string
    JudgeUsage  *model.Usage
    JudgeLatency time.Duration
}
```

### Route() 三段逻辑改为"解析队列为链"

新增内部辅助 `resolveGroupChain(name string) ([]*store.Model, error)`:**只查 `ModelGroup`**(命中则取其启用且 Provider 启用的模型,按 Position 升序;过滤后为空则视为不可用);**不查 `Model`**。未命中队列名返回"未找到"错误。

- **Override**:`req.Override` 非空时经 `resolveGroupChain` 解析;未命中队列名 -> **直接返回错误**(不回退 judge)。
- **Judge**:候选改为 `[]Candidate{Name, Description}`,**只含启用且链非空的队列**;`BuildJudgeMessages` 与 `ParseJudgeOutput` 改用 `Candidate`;判定结果再 `resolveGroupChain`。judge 未命中候选或不可用 -> 落到 fallback。
- **Fallback**:`DefaultGroupID` 指向的队列经 `resolveGroupChain` 解析;未配置或链空 -> 报错"无可用队列"。

链为空时:explicit override -> 报错;judge 命中但链空 -> 不作为候选(见上);fallback 链空 -> 报错。

### Candidate 与 judge 函数签名

```go
type Candidate struct {
    Name        string
    Description string
}
func BuildJudgeMessages(candidates []Candidate, userText string) []model.Message
func ParseJudgeOutput(raw string, known []string) string  // known = 队列名并集
```

**接口签名变更(破坏性)**:`JudgeClient.Judge` 的 `candidates []store.Model` 改为 `candidates []Candidate`。由此需同步适配:
- `internal/routing/judge_client.go` 的 `defaultJudgeClient.Judge`
- `internal/server/server.go` 的 `lazyJudge.Judge`
- 现有测试中的 JudgeClient fake(`engine_test.go` / `judge_client_test.go` 等)

**判定模型自身**:仍是单个模型,不作为"队列"出现,也**不再是路由候选**(候选只有队列)。judge 通过 `GetJudgeModel()`(按 `is_judge`)单独获取,与队列候选互不影响。

**StoreDeps 接口调整**:`ListEnabledModels` 不再用于候选;新增 `ListEnabledModelGroups`、`GetModelGroupByName`、`GetModelGroup`、`GetGroupItemsOrdered`。`GetModel`/`GetModelByName` 仅用于 judge 模型自身与网关取 Provider。

## 5. 网关失败转移循环（`internal/server/gateway.go`）

`handleChat` / `streamResponse` 改为遍历 `dec.Models`。`Dispatcher` 完全不改,仍只负责单端点调用 + 其内部 `isRetryable` 重试。

### 非流式

```go
var lastErr error
var retryCount int
for i, m := range dec.Models {
    prov := getProvider(m.ProviderID); apiKey := decrypt(...)
    req.Model = m.Name
    body := buildBody(req, prov.Protocol)   // 按各模型协议单独构建
    resp, rc, err := CallWithRetry(ctx, prov.BaseURL, apiKey, prov.Protocol, body, prov.RetryMax, prov.RetryBackoffMs)
    retryCount = rc
    if err == nil {
        dec.ServedModel = m.Name
        encode + respond; break
    }
    lastErr = err  // 任意错误都转向下一个;最后一个失败才报错
}
// lastErr != nil 且未成功 -> writeGatewayError + writeLog(失败)
```

### 流式

同样遍历,用网关侧 `started` 标志(在 `onChunk` 首次回调置 true)判断:

- `CallStreamWithRetry` 返回 err 且 `!started` -> 首字节前失败,可转移下一个模型。
- `started` 已 true -> 已输出内容,不可转移(避免重复内容),立即返回错误(沿用现有行为)。
- encoder 按当前尝试的 `m.Name` 创建(只有成功的那个会真正输出 chunk)。

### 转移计数

`FailoverCount` = 成功前尝试过的模型数(成功模型在链中的下标)。全失败时 = 尝试总数-1。

## 6. Judge 集成 + /v1/models

- **Judge 候选**:引擎组装**仅启用且链非空的队列**(各带描述)。判定模型自身不再是候选。
- **`/v1/models` (`handleListModels`)**:**只列出启用且链非空的队列名**(不再列单个模型),让客户端发现 `deepseek-v4-flash` 这类目标。

## 7. 日志改动（`internal/store/logs.go`）

`RequestLog` 新增两列(`AutoMigrate` 自动加列):

- `ServedModel string` - 实际服务的模型名(队列=成功的那个;全失败=最后尝试的模型名)。
- `FailoverCount int` - 转移次数(0=首模型即成功)。

`RoutedModel` = 命中的队列名。`RetryCount` = 成功模型自身重试次数(全失败时为最后模型重试次数)。

`TokenStatsByModel` / `TokenStatsByProvider` 改为按 `COALESCE(served_model, routed_model)` 聚合,确保队列请求的 token 正确归因到真实模型/Provider,且兼容旧数据(served_model 为空时回退 routed_model)。

## 8. RoutingConfig 与默认兜底（`internal/store/routing.go`）

`RoutingConfig` 字段变更:

- `DefaultModelID *uint` -> **`DefaultGroupID *uint`**(指向 `ModelGroup`)。
- `JudgeModelID *uint` 保留不变(仍指单个 judge 模型)。
- `UpdateRoutingConfig` 的 `is_judge` 镜像逻辑不变;默认兜底校验改为 group 存在性。

**迁移说明**:GORM `AutoMigrate` 会新增 `DefaultGroupID` 列但**不会删除**旧 `DefaultModelID` 列(残留无害,代码不再读取)。现有部署的旧默认模型配置失效,需在管理后台重新设置默认队列。`IsModelReferenced` 不再检查 default(因 default 已是 group):仅当模型是当前 judge(`is_judge=true`)或被 `routing_config.judge_model_id` 引用时阻塞删除;被队列引用则级联删除 `ModelGroupItem`(软引用)。

## 9. 删除安全

- **删除模型**:级联删除引用它的 `ModelGroupItem`(队列引用是软引用,不阻塞删除)。`IsModelReferenced` 仅 judge 相关引用阻塞删除(见第 8 节)。
- **删除队列**:级联删除其 `ModelGroupItem`;若该队列是 `RoutingConfig.DefaultGroupID`,阻塞删除(409)。
- **删除 judge 模型**:仍阻塞(现状不变)。

## 10. Admin API

### 队列路由组 `/admin/groups`(沿用现有 admin 鉴权)

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/admin/groups` | 列出所有队列(含 item 数量) |
| POST | `/admin/groups` | 创建队列 `{name, display_name, description, enabled}` |
| PUT | `/admin/groups/:id` | 更新队列元信息 |
| DELETE | `/admin/groups/:id` | 删除队列(级联 items;默认队列阻塞 409) |
| GET | `/admin/groups/:id/items` | 取有序 items(含模型详情) |
| PUT | `/admin/groups/:id/items` | 整体替换有序列表,body `{"items":[model_id1, model_id2,...]}`,数组顺序=Position |

整体替换 items 让前端增/删/排序都通过一次 PUT 完成。后端按数组下标重写 Position,自动消除位置冲突。校验:model_id 必须存在;同一队列内去重(重复取首次出现)。

### 路由配置端点改动(`/admin/routing`)

`GET` / `PUT /admin/routing` 的 `default_model_id` 字段改为 `default_group_id`;`judge_model_id` 不变。`PUT` 校验 group 存在性。

## 11. 前端（`web/src`）

新增菜单项"模型队列"(`/queues`),新增 `pages/Queues.tsx` + `api/groups.ts`,`Layout.tsx` 加菜单、`App.tsx` 加路由。沿用 react-query + Ant Design 风格,与 `Sources.tsx` 一致。

### 队列管理页结构

- 队列表格:名称、展示名、描述、模型数、启用开关、操作[管理成员/编辑/删除]
- 新建/编辑队列弹窗:名称、展示名、描述、启用
- **管理成员弹窗**:左侧"可选模型"(所有启用模型,下拉选择添加),右侧"队列成员"(有序列表,每项带 ↑/↓ 调序 + 移除);保存时一次 `PUT /items`(数组顺序即 Position)

### 路由配置页改动(`Routing.tsx`)

- "默认兜底"选择器从模型下拉改为**队列下拉**(列启用队列)。
- `api/routing.ts` 的 `RoutingConfig` 类型:`default_model_id` -> `default_group_id`。
- judge 模型选择器不变(仍选单个模型)。

### API 模块（`api/groups.ts`）

```ts
export interface ModelGroup {
  id: number; name: string; display_name: string
  description: string; enabled: boolean; item_count?: number
}
export interface GroupItem { id: number; group_id: number; model_id: number; position: number; model?: Model }
export async function listGroups(): Promise<ModelGroup[]>
export async function createGroup(g: Partial<ModelGroup>): Promise<ModelGroup>
export async function updateGroup(id: number, g: Partial<ModelGroup>): Promise<ModelGroup>
export async function deleteGroup(id: number): Promise<void>
export async function listGroupItems(id: number): Promise<GroupItem[]>
export async function replaceGroupItems(id: number, modelIds: number[]): Promise<void>
```

## 12. 文件变化总览

| 文件 | 操作 |
|------|------|
| `internal/store/groups.go` | **新建** - ModelGroup/ModelGroupItem 模型与 Store 方法 |
| `internal/store/store.go` | **修改** - AutoMigrate 注册两张表 |
| `internal/store/routing.go` | **修改** - RoutingConfig: DefaultModelID -> DefaultGroupID;UpdateRoutingConfig 校验 |
| `internal/store/logs.go` | **修改** - RequestLog 加 ServedModel/FailoverCount;stats 按 served_model 聚合 |
| `internal/store/models.go` | **修改** - DeleteModel 级联删除 ModelGroupItem;IsModelReferenced 仅判 judge |
| `internal/routing/engine.go` | **修改** - Decision 加链;Route() 解析队列为链;resolveGroupChain;StoreDeps 加 group 方法 |
| `internal/routing/judge.go` | **修改** - Candidate;BuildJudgeMessages/ParseJudgeOutput 签名 |
| `internal/routing/judge_client.go` | **修改** - 适配 Candidate 签名 |
| `internal/server/gateway.go` | **修改** - 遍历链的失败转移循环(流式/非流式);handleListModels 只列队列;writeLog 加字段 |
| `internal/server/admin.go` | **修改** - 新增 groups CRUD + items 端点;routing 端点 default_model_id -> default_group_id |
| `internal/server/server.go` | **修改** - 注册 /admin/groups 路由;lazyJudge 适配 Candidate |
| `web/src/api/groups.ts` | **新建** |
| `web/src/api/routing.ts` | **修改** - default_model_id -> default_group_id |
| `web/src/pages/Queues.tsx` | **新建** |
| `web/src/pages/Routing.tsx` | **修改** - 默认兜底选择器改为队列 |
| `web/src/components/Layout.tsx` | **修改** - 加菜单项 |
| `web/src/App.tsx` | **修改** - 加 /queues 路由 |

## 13. 测试要点

- Store:群组 CRUD、ReplaceGroupItems 顺序与去重、删除级联、默认队列删除阻塞。
- Engine:`resolveGroupChain` 对队列/空链/未命中的处理(未命中报错);judge 候选只含队列名时 `ParseJudgeOutput` 命中队列;override 未命中队列名直接报错;`JudgeClient` 接口签名变更后所有 fake 适配编译通过。
- Gateway:非流式按序转移(mock 多个 Provider,前 N 个失败);流式首字节前转移、首字节后不转移;`ServedModel`/`FailoverCount` 回填;全失败返回最后错误;显式非队列名返回错误。
- 兼容性:auto/route 触发 judge;空 model 触发 judge。

## 14. 不在范围内

- 不做前端拖拽排序(用 ↑/↓ 按钮)。
- 不新增前端测试(与现有 Plan 一致)。
- 不改 `Dispatcher` 内部实现。
- 不自动迁移旧 `DefaultModelID` 配置(需手动重设默认队列)。
