# 判定模型改造为判定队列设计

**日期：** 2026-07-31
**状态：** 已确认，待实施
**关联：** 基于 `2026-07-31-model-queue-failover-design.md` 队列失败转移基础设施

## 1. 背景与动机

当前智能路由的判定（judge）由**单个模型**承担：`RoutingConfig.JudgeModelID *uint` 指向一个模型，`Model.IsJudge` 标记互斥的唯一判定模型，`Engine.Route()` 通过 `GetJudgeModel()` 取出后调用一次 `JudgeClient.Judge(judgeModel, ...)`。判定调用失败（超时/错误/空输出）即直接回退到默认兜底队列，判定本身没有失败转移能力。

业务请求侧已具备"模型队列 + 有序失败转移"能力（见 `2026-07-31-model-queue-failover-design.md`），但判定侧仍是单点。本设计将判定由"单模型"升级为"判定队列（judge queue）"：`JudgeModelID` 改为 `JudgeGroupID` 指向一个 `ModelGroup`，判定调用按队列内模型顺序依次尝试，全部失败再回退到默认兜底队列。由此判定获得与业务一致的失败转移能力，旧的单模型判定设施（`is_judge` 字段、`SetJudgeModel`/`GetJudgeModel`、`POST /admin/models/:id/judge` 端点）全部移除。

## 2. 关键决策（已与用户确认）

1. **判定改为队列**：`RoutingConfig.JudgeModelID` 重命名为 `JudgeGroupID`，指向 `ModelGroup`；判定调用遍历队列链，首个成功即返回。
2. **判定失败转移语义**：判定队列内按序重试（首个失败/空输出→下一个），全部失败再回退到默认兜底队列——与业务队列语义一致。
3. **失败转移封装位置（方案 A）**：失败转移循环封装在 `JudgeClient` 内，`JudgeClient.Judge` 接收整条判定链并返回实际成功的模型名；`lazyJudge` 在内部逐模型解析 provider 并重试。引擎 `Route` 只调一次 `Judge`，err 即走兜底。
4. **旧设施全部删除**：移除 `Model.IsJudge` 字段、`SetJudgeModel`/`GetJudgeModel`/`IsModelReferenced`、`POST /admin/models/:id/judge` 端点。判定完全由 `judge_group_id` 驱动。
5. **自动迁移旧配置**：升级时若存在遗留 `judge_model_id`，自动创建一个判定队列（含旧判定模型）并指向它，老部署无感升级；迁移幂等。
6. **判定队列不作为服务候选**：构建 judge 候选队列列表时，排除 `JudgeGroupID` 指向的判定队列本身（判定队列用于判定，不对外提供生成服务，避免循环）。

## 3. 数据模型与迁移

### `RoutingConfig` 字段变更（`internal/store/routing.go`）

```go
type RoutingConfig struct {
    ID                 uint  `gorm:"primaryKey" json:"id"`
    JudgeGroupID       *uint `json:"judge_group_id"`        // 由 JudgeModelID 重命名，指向 ModelGroup
    DefaultGroupID     *uint `json:"default_group_id"`
    JudgeMaxInputChars int   `gorm:"default:2000" json:"judge_max_input_chars"`
}

func (s *Store) UpdateRoutingConfig(rc *RoutingConfig) error {
    rc.ID = 1
    return s.DB.Save(rc).Error   // 删除原 is_judge 镜像逻辑
}
```

### `Model` 与 store 方法（`internal/store/models.go`）

- 移除 `Model.IsJudge` 字段。
- 移除 `SetJudgeModel`、`GetJudgeModel`、`IsModelReferenced`（判定已无单模型硬引用；队列成员关系是软引用，删除模型时级联清理 `ModelGroupItem`，与业务队列一致）。

### 迁移函数（`internal/store/store.go`）

新增 `migrateLegacyJudge(db *gorm.DB) error`，在 `Open` 中 `AutoMigrate` + seed 单例行之后执行。

**关键正确性**：旧引擎 `GetJudgeModel()` 的判定模型来源是 `models.is_judge = true AND enabled = true`（真正的单一事实源），而非 `routing_configs.judge_model_id`——后者仅由 `UpdateRoutingConfig` 镜像写入，且 `POST /admin/models/:id/judge`（`SetJudgeModel`）只改 `is_judge` 不改 `judge_model_id`，两者可能不一致。因此迁移**以 `is_judge` 为首选源**，`judge_model_id` 仅作回退。

流程：

1. 驱动无关检测：用 `db.Migrator().ColumnTypes(&Model{})` 与 `ColumnTypes(&RoutingConfig{})` 判断 `models.is_judge`、`routing_configs.judge_model_id` 两列是否仍存在。两者都已不存在 → 直接返回 nil（幂等）。
2. 确定旧判定模型 ID（按优先级）：
   - 首选：`SELECT id FROM models WHERE is_judge = true AND enabled = true LIMIT 1`（旧引擎实际使用的那个）。
   - 回退：若上者为空且 `routing_configs.judge_model_id` 列存在，raw SQL 读出 id=1 的 `judge_model_id`。
3. 读 `routing_configs.judge_group_id`（若列存在）。当步骤 2 得到旧判定模型且 `judge_group_id` 为空时：创建判定队列（名 `judge`，重名则 `judge-2`/`judge-3`…直至不重名，`Remark: "migrated from legacy judge model"`，`Enabled: true`），把旧判定模型加入为 `ModelGroupItem{Position: 0}`，写回 `judge_group_id`。若 `judge_group_id` 已有值（部分迁移过）则跳过创建，仅落清理。
4. 丢弃遗留列：`db.Migrator().DropColumn(&RoutingConfig{}, "judge_model_id")`、`DropColumn(&Model{}, "is_judge")`，调用前各做 `HasColumn` 兜底。
5. 幂等：再次启动时两列均已不存在 → 步骤 1 直接返回 nil。

**SQLite 要求**：`DROP COLUMN` 需 SQLite ≥ 3.35（2021-03-12），项目所用现代驱动均满足。

## 4. 路由引擎改动（`internal/routing`）

### StoreDeps 接口（`engine.go`）

移除 `GetJudgeModel() (*store.Model, error)`；其余方法保留（`GetRoutingConfig`、`ListEnabledModelGroups`、`GetModelGroup`、`GetModelGroupByName`、`GetGroupChain`）。引擎用现成的 `GetModelGroup` + `GetGroupChain` 解析判定队列链。

### JudgeClient 接口（`engine.go`）

```go
type JudgeClient interface {
    Judge(chain []*store.Model, candidates []Candidate, userText string) (raw string, servedModel string, usage *model.Usage, err error)
}
```

`Decision.JudgeModel` 字段保留，现由返回的 `servedModel` 填充（失败转移后实际成功的判定模型名，用于日志）。

### Route() 判定段流程

```go
rc, err := e.Store.GetRoutingConfig()
// 解析判定队列链
var judgeChain []*store.Model
if rc.JudgeGroupID != nil {
    if g, err := e.Store.GetModelGroup(*rc.JudgeGroupID); err == nil && g != nil && g.Enabled {
        if ch, err := e.Store.GetGroupChain(g.ID); err == nil && len(ch) > 0 {
            judgeChain = toPtrChain(ch)
        }
    }
}
if len(judgeChain) > 0 {
    // 构建候选：排除判定队列自身
    groups, _ := e.Store.ListEnabledModelGroups()
    cands, known := ... // 跳过 g.ID == *rc.JudgeGroupID 的队列
    userText := TruncateUserText(req.LastUserMessage(), rc.JudgeMaxInputChars)
    raw, servedName, usage, jerr := e.Judge.Judge(judgeChain, cands, userText)
    // jerr != nil -> 日志 + 走兜底
    // raw 可解析 -> 返回 judge 决策（JudgeModel = servedName）
    // raw 不可解析 -> 日志 + 走兜底
}
// 3. Fallback：DefaultGroupID 链（不变）
```

### `defaultJudgeClient`（`judge_client.go`）

`defaultJudgeClient.Judge(judgeModel *store.Model, candidates, userText)` 单模型 HTTP 调用**签名不变**，降级为内部辅助（不再实现 `JudgeClient` 接口，移除其 `var _ JudgeClient = (*defaultJudgeClient)(nil)` 编译期断言）。`NewJudgeClient` 构造器保留。

### `lazyJudge`（`internal/server/server.go`）—— 失败转移落点

```go
func (l *lazyJudge) Judge(chain []*store.Model, candidates []routing.Candidate, userText string) (string, string, *model.Usage, error) {
    var lastErr error
    for _, jm := range chain {
        prov, err := l.st.GetProvider(jm.ProviderID)
        if err != nil { lastErr = err; continue }
        apiKey, _ := store.Decrypt(l.key, prov.APIKey)
        raw, usage, err := routing.NewJudgeClient(l.disp, prov.BaseURL, apiKey, prov.Protocol, prov.ProxyURL).Judge(jm, candidates, userText)
        if err == nil && raw != "" {
            return raw, jm.Name, usage, nil   // 成功
        }
        if err != nil { lastErr = err }       // nil err + 空 raw 也视为失败，继续下一个
    }
    return "", "", nil, fmt.Errorf("judge queue exhausted: %w", lastErr)
}
```

**契约**：成功 ⇔ `err == nil && raw != ""`。空输出也触发下一个判定模型（契合"队列内重试再兜底"），引擎只需检查 `err`。

## 5. Admin API 与端点清理（`internal/server`）

### 路由注册（`server.go`）

移除 `authAdmin.POST("/models/:id/judge", app.handleSetJudge)`。

### admin.go

- 删除 `handleSetJudge` 处理函数。
- `handleGetRouting` / `handleUpdateRouting`：响应体与请求体字段 `judge_model_id` → `judge_group_id`；`PUT` 校验复用现有"默认兜底队列"模式（`GetModelGroup` 存在且 `Enabled`）。
- `handleDeleteModel`：移除 `IsModelReferenced` 检查，直接 `DeleteModel`（成员关系级联删除，与业务队列一致）。

## 6. 前端（`web/src`）

| 文件 | 改动 |
|------|------|
| `api/routing.ts` | `RoutingConfig.judge_model_id` → `judge_group_id` |
| `api/models.ts` | 删 `is_judge` 字段、删 `setJudgeModel()` 函数 |
| `pages/Routing.tsx` | 表单项 `judge_model_id` → `judge_group_id`；label "判定模型" → "判定队列"；选项改用 `groupOptions`（启用队列）；tooltip/extra 文案同步（改为"判定队列，按队列内模型顺序失败转移"）；移除不再使用的 `modelOptions` |
| `pages/Sources.tsx` | 删 `judgeModelId` 派生与"判定"列（判定现由队列承担，成员关系在队列管理页可见） |
| `pages/Logs.tsx` | `judge_model` 字段保留（现=实际执行判定的模型），文案不变 |
| `api/logs.ts` | 无改动（`judge_model` 字段保留） |

## 7. 日志、文档与测试

### 日志

`internal/server/gateway.go` 现有代码读 `dec.JudgeModel` 写入 `RequestLog.JudgeModel`（L235），无需改动；现记录的是失败转移后实际成功的判定模型名。`RequestLog` 结构体不变。

### 文档

- `README.md`：更新"使用"步骤 3（设判定队列而非模型，移除 `POST /admin/models/:id/judge` 引用）；"功能特性"中"由判定模型"改"由判定队列"。
- `MEMORY.md` / 跨会话 `project_memory.md`：按 AGENTS.md 5.5 同步（强制）。

### 测试

- `internal/routing/engine_test.go`：`fakeStore` 移除 `GetJudgeModel`；通过 `GetRoutingConfig` 返回 `JudgeGroupID` + 可解析链（复用现有 `chains` map）；`fakeJudge.Judge` 签名改为 `(chain, ...) (raw, servedName, usage, err)`；用例改为设置判定队列。
- `internal/routing/judge_client_test.go`：不变（`defaultJudgeClient.Judge` 单模型签名未动）。
- `internal/server/apptest_test.go` / `gateway_test.go`：用"建判定队列 + `JudgeGroupID`"替换 `SetJudgeModel(judge.ID)` + `JudgeModelID: &judge.ID`。

## 8. 文件变化总览

| 文件 | 操作 |
|------|------|
| `internal/store/routing.go` | **修改** - `JudgeModelID` → `JudgeGroupID`；`UpdateRoutingConfig` 删 is_judge 镜像 |
| `internal/store/models.go` | **修改** - 删 `IsJudge` 字段、`SetJudgeModel`/`GetJudgeModel`/`IsModelReferenced` |
| `internal/store/store.go` | **修改** - 新增 `migrateLegacyJudge`，`Open` 中调用 |
| `internal/routing/engine.go` | **修改** - `StoreDeps` 去 `GetJudgeModel`；`JudgeClient` 接口改链式；`Route` 判定段改解析队列 + 排除判定队列候选 |
| `internal/routing/judge_client.go` | **修改** - `defaultJudgeClient` 降级为内部辅助，移除接口断言 |
| `internal/server/server.go` | **修改** - 移除 `/models/:id/judge` 路由；`lazyJudge.Judge` 改链式失败转移 |
| `internal/server/admin.go` | **修改** - 删 `handleSetJudge`；routing 端点字段重命名；`handleDeleteModel` 去 `IsModelReferenced` |
| `internal/server/gateway.go` | 无改动（`dec.JudgeModel` 读取不变） |
| `web/src/api/routing.ts` | **修改** - 字段重命名 |
| `web/src/api/models.ts` | **修改** - 删 `is_judge`、`setJudgeModel` |
| `web/src/pages/Routing.tsx` | **修改** - 判定选择器改为队列 |
| `web/src/pages/Sources.tsx` | **修改** - 删判定列与派生 |
| `README.md` | **修改** - 步骤 3 与功能特性文案 |
| `MEMORY.md` / `project_memory.md` | **修改** - 同步记忆 |
| `internal/routing/engine_test.go` | **修改** - fakeStore/fakeJudge 适配 |
| `internal/server/apptest_test.go` / `gateway_test.go` | **修改** - 测试改为建判定队列 |

## 9. 不在范围内

- 不做判定队列与业务队列的隔离校验（管理员可让同一模型同时属于判定队列与业务队列，由其自行负责）。
- 不为"判定队列为空/被删"做特殊阻塞：判定队列链为空时引擎跳过判定直接走兜底（与管理员预期一致）。
- 不改 `Dispatcher` 内部实现、不改网关失败转移循环。
- 不新增前端测试（与既有 Plan 一致）。
