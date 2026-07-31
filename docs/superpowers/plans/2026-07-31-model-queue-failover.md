# 模型队列(有序失败转移)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 引入"模型队列"作为对外唯一可路由目标,按序请求队列内模型,失败转移下一个,与判定模型智能路由兼容。

**Architecture:** 新增 `ModelGroup`/`ModelGroupItem` 两表;`Engine.Route()` 把目标(队列)解析为有序模型链 `Decision.Models`;网关遍历链做失败转移(单模型仍用 Provider 的 `RetryMax` 重试);judge 候选只含队列名;默认兜底由 `DefaultModelID` 改为 `DefaultGroupID`。

**Tech Stack:** Go + GORM/SQLite + Gin(后端);React + TypeScript + Ant Design + react-query(前端)。测试用 `testify`。

**Spec:** `docs/superpowers/specs/2026-07-31-model-queue-failover-design.md`

**关键约定:** 旧 `RoutingConfig.DefaultModelID` 字段在 Task 2 保留(标记 deprecated)以减小跨包破坏面,Task 6 收尾时删除。每个 Task 结束时 `go build ./...` 与 `go test ./...` 均须通过。

---

## Task 1: Store - 队列模型与 CRUD(纯增量)

**Files:**
- Create: `internal/store/groups.go`
- Create: `internal/store/groups_test.go`
- Modify: `internal/store/store.go:43`(AutoMigrate 注册两张表)

- [ ] **Step 1: 写 `internal/store/groups.go`**

```go
package store

import (
	"time"

	"gorm.io/gorm"
)

// ModelGroup 是对外可路由的具名队列,映射到一组有序 Model。
type ModelGroup struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	DisplayName string    `gorm:"not null" json:"display_name"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

// ModelGroupItem 是队列内模型的有序关联,Position 升序即请求顺序。
type ModelGroupItem struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	GroupID  uint `gorm:"not null;uniqueIndex:idx_group_model,priority:1;uniqueIndex:idx_group_pos,priority:1" json:"group_id"`
	ModelID  uint `gorm:"not null;uniqueIndex:idx_group_model,priority:2" json:"model_id"`
	Position int  `gorm:"uniqueIndex:idx_group_pos,priority:2" json:"position"`
}

func (s *Store) ListModelGroups() ([]ModelGroup, error) {
	var gs []ModelGroup
	err := s.DB.Order("id desc").Find(&gs).Error
	return gs, err
}

func (s *Store) ListEnabledModelGroups() ([]ModelGroup, error) {
	var gs []ModelGroup
	err := s.DB.Where("enabled = ?", true).Find(&gs).Error
	return gs, err
}

func (s *Store) GetModelGroup(id uint) (*ModelGroup, error) {
	var g ModelGroup
	if err := s.DB.First(&g, id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) GetModelGroupByName(name string) (*ModelGroup, error) {
	var g ModelGroup
	err := s.DB.Where("name = ?", name).First(&g).Error
	return &g, err
}

func (s *Store) CreateModelGroup(g *ModelGroup) error {
	return s.DB.Create(g).Error
}

func (s *Store) UpdateModelGroup(g *ModelGroup) error {
	return s.DB.Save(g).Error
}

// DeleteModelGroup 删除队列并级联删除其 items。
func (s *Store) DeleteModelGroup(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&ModelGroupItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ModelGroup{}, id).Error
	})
}

// GetGroupItemsOrdered 返回队列内 items,按 Position 升序。
func (s *Store) GetGroupItemsOrdered(groupID uint) ([]ModelGroupItem, error) {
	var items []ModelGroupItem
	err := s.DB.Where("group_id = ?", groupID).Order("position asc").Find(&items).Error
	return items, err
}

// GetGroupChain 返回队列内启用且其 Provider 启用的模型,按 Position 升序。
// 这是路由引擎实际尝试的有序链。
func (s *Store) GetGroupChain(groupID uint) ([]Model, error) {
	items, err := s.GetGroupItemsOrdered(groupID)
	if err != nil {
		return nil, err
	}
	var chain []Model
	for _, it := range items {
		var m Model
		if err := s.DB.First(&m, it.ModelID).Error; err != nil {
			continue
		}
		if !m.Enabled {
			continue
		}
		var p Provider
		if err := s.DB.First(&p, m.ProviderID).Error; err != nil {
			continue
		}
		if !p.Enabled {
			continue
		}
		chain = append(chain, m)
	}
	return chain, nil
}

// ReplaceGroupItems 事务内整体替换队列 items,modelIDs 数组下标即 Position。
// 自动去重(重复取首次出现),跳过 0。
func (s *Store) ReplaceGroupItems(groupID uint, modelIDs []uint) error {
	seen := make(map[uint]bool)
	uniq := make([]uint, 0, len(modelIDs))
	for _, id := range modelIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&ModelGroupItem{}).Error; err != nil {
			return err
		}
		for i, mid := range uniq {
			if err := tx.Create(&ModelGroupItem{GroupID: groupID, ModelID: mid, Position: i}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// CountGroupsByModel 返回引用该模型的队列数(诊断/删除提示用)。
func (s *Store) CountGroupsByModel(modelID uint) (int64, error) {
	var n int64
	err := s.DB.Model(&ModelGroupItem{}).Where("model_id = ?", modelID).Count(&n).Error
	return n, err
}
```

- [ ] **Step 2: 注册迁移**

修改 `internal/store/store.go` 第 43 行,把两张新表加入 AutoMigrate:

```go
	if err := db.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &RequestLog{}, &Setting{}, &ModelGroup{}, &ModelGroupItem{}); err != nil {
```

- [ ] **Step 3: 写失败测试 `internal/store/groups_test.go`**

```go
package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelGroupCRUD(t *testing.T) {
	s := newTestStore(t)
	g := &ModelGroup{Name: "deepseek-v4-flash", DisplayName: "DSV4", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))
	assert.NotZero(t, g.ID)

	got, err := s.GetModelGroupByName("deepseek-v4-flash")
	assert.NoError(t, err)
	assert.Equal(t, "DSV4", got.DisplayName)

	got.Description = "fast"
	assert.NoError(t, s.UpdateModelGroup(got))
	assert.NoError(t, s.DeleteModelGroup(got.ID))
}

func TestReplaceGroupItemsOrderAndDedup(t *testing.T) {
	s := newTestStore(t)
	prov := &Provider{Name: "p", BaseURL: "http://x", Protocol: "openai", Enabled: true}
	assert.NoError(t, s.CreateProvider(prov))
	m1 := &Model{Name: "m1", DisplayName: "1", ProviderID: prov.ID, Enabled: true}
	m2 := &Model{Name: "m2", DisplayName: "2", ProviderID: prov.ID, Enabled: true}
	assert.NoError(t, s.CreateModel(m1))
	assert.NoError(t, s.CreateModel(m2))
	g := &ModelGroup{Name: "q", DisplayName: "Q", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))

	// 重复 m2 应去重,顺序保留首次出现
	assert.NoError(t, s.ReplaceGroupItems(g.ID, []uint{m2.ID, m1.ID, m2.ID}))

	items, err := s.GetGroupItemsOrdered(g.ID)
	assert.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, m2.ID, items[0].ModelID)
	assert.Equal(t, 0, items[0].Position)
	assert.Equal(t, m1.ID, items[1].ModelID)
	assert.Equal(t, 1, items[1].Position)
}

func TestGetGroupChainFiltersDisabled(t *testing.T) {
	s := newTestStore(t)
	prov := &Provider{Name: "p", BaseURL: "http://x", Protocol: "openai", Enabled: true}
	assert.NoError(t, s.CreateProvider(prov))
	on := &Model{Name: "on", DisplayName: "on", ProviderID: prov.ID, Enabled: true}
	off := &Model{Name: "off", DisplayName: "off", ProviderID: prov.ID, Enabled: false}
	assert.NoError(t, s.CreateModel(on))
	assert.NoError(t, s.CreateModel(off))
	g := &ModelGroup{Name: "q", DisplayName: "Q", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))
	assert.NoError(t, s.ReplaceGroupItems(g.ID, []uint{on.ID, off.ID}))

	chain, err := s.GetGroupChain(g.ID)
	assert.NoError(t, err)
	assert.Len(t, chain, 1)
	assert.Equal(t, "on", chain[0].Name)
}

func TestDeleteModelGroupCascadesItems(t *testing.T) {
	s := newTestStore(t)
	prov := &Provider{Name: "p", BaseURL: "http://x", Protocol: "openai", Enabled: true}
	assert.NoError(t, s.CreateProvider(prov))
	m := &Model{Name: "m", DisplayName: "m", ProviderID: prov.ID, Enabled: true}
	assert.NoError(t, s.CreateModel(m))
	g := &ModelGroup{Name: "q", DisplayName: "Q", Enabled: true}
	assert.NoError(t, s.CreateModelGroup(g))
	assert.NoError(t, s.ReplaceGroupItems(g.ID, []uint{m.ID}))

	assert.NoError(t, s.DeleteModelGroup(g.ID))
	items, _ := s.GetGroupItemsOrdered(g.ID)
	assert.Empty(t, items)
}
```

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/store/ -run 'TestModelGroup|TestReplaceGroupItems|TestGetGroupChain|TestDeleteModelGroup' -v`
Expected: PASS

- [ ] **Step 5: 全量编译 + 测试**

Run: `go build ./... && go test ./...`
Expected: 全部 PASS(纯增量,无破坏)

- [ ] **Step 6: 提交**

```bash
git add internal/store/groups.go internal/store/groups_test.go internal/store/store.go
git commit -m "feat(store): add ModelGroup and ModelGroupItem with CRUD"
```

---

## Task 2: Store - RoutingConfig 默认队列 + 日志字段 + 模型级联

**Files:**
- Modify: `internal/store/routing.go`(加 `DefaultGroupID`,保留 `DefaultModelID` 标 deprecated)
- Modify: `internal/store/models.go`(`DeleteModel` 级联 items;`IsModelReferenced` 仅判 judge)
- Modify: `internal/store/logs.go`(加 `ServedModel`/`FailoverCount`;stats 用 served_model)
- Modify: `internal/store/store_test.go`(AutoMigrate 断言)

- [ ] **Step 1: 改 `internal/store/routing.go`**

把 `RoutingConfig` 改为:

```go
type RoutingConfig struct {
	ID                 uint  `gorm:"primaryKey" json:"id"`
	JudgeModelID       *uint `json:"judge_model_id"`
	DefaultModelID     *uint `json:"default_model_id"` // deprecated: 保留列以兼容旧库,代码不再使用
	DefaultGroupID     *uint `json:"default_group_id"` // 默认兜底队列
	JudgeMaxInputChars int   `gorm:"default:2000" json:"judge_max_input_chars"`
}
```

`GetRoutingConfig` / `UpdateRoutingConfig` 逻辑不变(字段随 `Save`/`First` 自动处理)。`UpdateRoutingConfig` 的 `is_judge` 镜像逻辑保持不变。

- [ ] **Step 2: 改 `internal/store/models.go`**

`DeleteModel` 级联删除 `ModelGroupItem`;`IsModelReferenced` 移除 default 检查,仅判 judge:

```go
func (s *Store) DeleteModel(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("model_id = ?", id).Delete(&ModelGroupItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Model{}, id).Error
	})
}

// IsModelReferenced 报告该模型是否为当前判定模型(judge)或被
// routing_config.judge_model_id 引用。被队列引用属于软引用,删除时级联清理,
// 不阻塞。default 兜底已改为队列,故不再检查 default。
func (s *Store) IsModelReferenced(id uint) (bool, error) {
	var m Model
	if err := s.DB.First(&m, id).Error; err != nil {
		return false, err
	}
	if m.IsJudge {
		return true, nil
	}
	var rc RoutingConfig
	if err := s.DB.First(&rc, 1).Error; err != nil {
		return false, err
	}
	if rc.JudgeModelID != nil && *rc.JudgeModelID == id {
		return true, nil
	}
	return false, nil
}
```

- [ ] **Step 3: 改 `internal/store/logs.go`**

`RequestLog` 在 `RetryCount` 后加两列:

```go
	RetryCount       int    `json:"retry_count"`
	ServedModel      string `json:"served_model"`   // 实际服务的模型名(队列=成功的那个)
	FailoverCount    int    `json:"failover_count"` // 队列转移次数
	PromptTokens     int    `json:"prompt_tokens"`
```

`TokenStatsByModel` 与 `TokenStatsByProvider` 把聚合字段改为 `COALESCE(served_model, routed_model)`:

```go
func (s *Store) TokenStatsByModel() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	err := s.DB.Model(&RequestLog{}).
		Select("COALESCE(served_model, routed_model) as model, count(*) as count, sum(prompt_tokens) as prompt_tokens, sum(completion_tokens) as completion_tokens, sum(total_tokens) as total_tokens").
		Where("COALESCE(served_model, routed_model) != '' AND total_tokens > 0").
		Group("COALESCE(served_model, routed_model)").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}

func (s *Store) TokenStatsByProvider() ([]TokenStatRow, error) {
	var rows []TokenStatRow
	err := s.DB.Table("request_logs").
		Select("providers.name as provider, count(*) as count, sum(request_logs.prompt_tokens) as prompt_tokens, sum(request_logs.completion_tokens) as completion_tokens, sum(request_logs.total_tokens) as total_tokens").
		Joins("LEFT JOIN models ON COALESCE(request_logs.served_model, request_logs.routed_model) = models.name").
		Joins("LEFT JOIN providers ON models.provider_id = providers.id").
		Where("COALESCE(request_logs.served_model, request_logs.routed_model) != '' AND request_logs.total_tokens > 0 AND providers.name != ''").
		Group("providers.name").
		Order("total_tokens desc").
		Limit(10).
		Scan(&rows).Error
	return rows, err
}
```

- [ ] **Step 4: 更新 `internal/store/store_test.go`**

`TestOpenAutoMigrates` 的 AutoMigrate 断言加入新表:

```go
	err := s.DB.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &RequestLog{}, &Setting{}, &ModelGroup{}, &ModelGroupItem{})
```

`TestTokenAggregations` 既有断言不变(COALESCE 回退到 routed_model),仅确认通过。

- [ ] **Step 5: 更新 `internal/server/admin_test.go` 与 admin.go 注释**

Task 2 把 `IsModelReferenced` 改为不再检查 default,因此 `TestDeleteReferencedRejected` 中"删除默认模型 -> 409"的断言不再成立(默认已是队列,模型仅被队列软引用,删除级联清理)。把该块改为期望 200:

```go
	// Delete model id=2 (原默认模型,现仅被队列软引用) -> 200(级联清理 group items)
	req = httptest.NewRequest(http.MethodDelete, "/admin/models/2", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
```

同时更新 `internal/server/admin.go` `handleDeleteModel` 中的注释(约 344 行),去掉 `default_model_id` 提法:

```go
	// I10: reject deletion if the model is the judge or is referenced by
	// routing_config.judge_model_id. Queue membership is a soft reference
	// and is cascade-removed.
```

> 注:默认队列删除阻塞由 Task 5 的 `TestAdminGroupsCRUDAndItems` 覆盖。

- [ ] **Step 6: 编译 + 测试**

Run: `go build ./... && go test ./internal/store/ -v && go test ./internal/server/ -run TestDeleteReferencedRejected -v`
Expected: 全部 PASS。`routing`/`server` 其余包仍引用 `DefaultModelID`,但因字段保留,编译通过;engine 未改,server 用例仍按旧行为通过。

- [ ] **Step 7: 提交**

```bash
git add internal/store/routing.go internal/store/models.go internal/store/logs.go internal/store/store_test.go
git commit -m "feat(store): default group, served_model log fields, model cascade"
```

---

## Task 3: 路由引擎 - 队列唯一可路由 + 链式 Decision

**Files:**
- Modify: `internal/routing/judge.go`(Candidate + 签名)
- Modify: `internal/routing/engine.go`(StoreDeps、Decision、resolveGroupChain、Route)
- Modify: `internal/routing/judge_client.go`(适配签名)
- Modify: `internal/routing/judge_test.go`
- Modify: `internal/routing/judge_client_test.go`
- Modify: `internal/routing/engine_test.go`(重写 fakeStore/fakeJudge/用例)
- Modify: `internal/server/server.go`(lazyJudge 适配)
- Modify: `internal/server/apptest_test.go`(seed 队列 + DefaultGroupID)
- Modify: `internal/server/gateway_test.go`(mock 返回队列名)

- [ ] **Step 1: 改 `internal/routing/judge.go`**

新增 `Candidate`,改 `BuildJudgeMessages` 入参为 `[]Candidate`。`ParseJudgeOutput` 与 `TruncateUserText` 函数体保持不变。

```go
// Candidate 是给 judge 的可路由候选(队列),Name 是路由目标名。
type Candidate struct {
	Name        string
	Description string
}

const judgeSystemPrompt = `你是一个模型路由器。根据用户任务和可用队列列表,选择最合适的队列。
回复格式(共三行,不要输出额外内容):
- 第一行:队列名称(必须与列表中的某个名称完全一致)
- 第二行:[任务] 一句话总结用户在做什么任务
- 第三行:[理由] 一句话说明为什么选择该队列`

func BuildJudgeMessages(candidates []Candidate, userText string) []model.Message {
	var sb strings.Builder
	sb.WriteString("可用队列列表:\n")
	for _, c := range candidates {
		sb.WriteString("- ")
		sb.WriteString(c.Name)
		if c.Description != "" {
			sb.WriteString(" - ")
			sb.WriteString(c.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n用户任务:\n")
	sb.WriteString(userText)
	return []model.Message{
		{Role: "system", Content: judgeSystemPrompt},
		{Role: "user", Content: sb.String()},
	}
}
```

- [ ] **Step 2: 改 `internal/routing/engine.go`**

完整替换为:

```go
package routing

import (
	"fmt"
	"log"
	"time"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

// StoreDeps 是引擎所需的 *store.Store 子集。
type StoreDeps interface {
	GetJudgeModel() (*store.Model, error)
	GetRoutingConfig() (*store.RoutingConfig, error)
	ListEnabledModelGroups() ([]store.ModelGroup, error)
	GetModelGroup(id uint) (*store.ModelGroup, error)
	GetModelGroupByName(name string) (*store.ModelGroup, error)
	GetGroupChain(groupID uint) ([]store.Model, error)
}

var _ StoreDeps = (*store.Store)(nil)

// JudgeClient 调用判定模型选择一个队列名。
type JudgeClient interface {
	Judge(judgeModel *store.Model, candidates []Candidate, userText string) (string, *model.Usage, error)
}

type Decision struct {
	ModelName     string         // 目标名(队列名),用于日志 RoutedModel
	Model         *store.Model   // 链首模型(向后兼容)
	Models        []*store.Model // 有序链
	Reason        string         // override | judge | fallback
	ServedModel   string         // 实际服务模型名(网关回填)
	FailoverCount int            // 转移次数(网关回填)
	JudgeRaw      string
	JudgeModel    string
	JudgeUsage    *model.Usage
	JudgeLatency  time.Duration
}

type Engine struct {
	Store StoreDeps
	Judge JudgeClient
}

func New(s StoreDeps, j JudgeClient) *Engine {
	return &Engine{Store: s, Judge: j}
}

// resolveGroupChain 把队列名解析为有序模型链。只查 ModelGroup,不查 Model。
func (e *Engine) resolveGroupChain(name string) ([]*store.Model, error) {
	g, err := e.Store.GetModelGroupByName(name)
	if err != nil || g == nil || !g.Enabled {
		return nil, fmt.Errorf("queue %q not found", name)
	}
	chain, err := e.Store.GetGroupChain(g.ID)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("queue %q has no available models", name)
	}
	out := make([]*store.Model, len(chain))
	for i := range chain {
		out[i] = &chain[i]
	}
	return out, nil
}

func (e *Engine) Route(req *model.ChatRequest) (*Decision, error) {
	// 1. Override:必须是队列名,未命中直接报错(不回退)
	if req.Override != "" {
		chain, err := e.resolveGroupChain(req.Override)
		if err != nil {
			return nil, err
		}
		return &Decision{ModelName: req.Override, Model: chain[0], Models: chain, Reason: "override"}, nil
	}

	// 2. Judge:候选只含启用且链非空的队列
	rc, err := e.Store.GetRoutingConfig()
	if err != nil {
		return nil, fmt.Errorf("get routing config: %w", err)
	}
	judge, _ := e.Store.GetJudgeModel()
	judgeName := ""
	judgeRaw := ""
	var judgeUsage *model.Usage
	var judgeLatency time.Duration
	if judge != nil {
		judgeName = judge.Name
		groups, _ := e.Store.ListEnabledModelGroups()
		cands := make([]Candidate, 0, len(groups))
		known := make([]string, 0, len(groups))
		for _, g := range groups {
			ch, err := e.Store.GetGroupChain(g.ID)
			if err != nil || len(ch) == 0 {
				continue
			}
			cands = append(cands, Candidate{Name: g.Name, Description: g.Description})
			known = append(known, g.Name)
		}
		userText := TruncateUserText(req.LastUserMessage(), rc.JudgeMaxInputChars)
		jStart := time.Now()
		raw, usage, jerr := e.Judge.Judge(judge, cands, userText)
		judgeLatency = time.Since(jStart)
		judgeUsage = usage
		switch {
		case jerr != nil:
			log.Printf("[WARN] judge call failed: %v", jerr)
			judgeRaw = "error: " + jerr.Error()
		case raw == "":
			log.Printf("[WARN] judge returned empty output")
			judgeRaw = "error: empty judge output"
		default:
			if picked := ParseJudgeOutput(raw, known); picked != "" {
				if chain, err := e.resolveGroupChain(picked); err == nil {
					return &Decision{ModelName: picked, Model: chain[0], Models: chain, Reason: "judge", JudgeRaw: raw, JudgeModel: judgeName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency}, nil
				}
			}
			log.Printf("[WARN] judge output unparseable: %q", raw)
			judgeRaw = raw
		}
	}

	// 3. Fallback:默认队列
	if rc.DefaultGroupID != nil {
		if g, err := e.Store.GetModelGroup(*rc.DefaultGroupID); err == nil && g != nil && g.Enabled {
			if chain, err := e.Store.GetGroupChain(g.ID); err == nil && len(chain) > 0 {
				out := make([]*store.Model, len(chain))
				for i := range chain {
					out[i] = &chain[i]
				}
				return &Decision{ModelName: g.Name, Model: out[0], Models: out, Reason: "fallback", JudgeRaw: judgeRaw, JudgeModel: judgeName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency}, nil
			}
		}
	}
	return nil, fmt.Errorf("no queue available and no default configured")
}
```

- [ ] **Step 3: 改 `internal/routing/judge_client.go` 签名**

把 `Judge` 方法第二参从 `candidates []store.Model` 改为 `candidates []Candidate`,内部 `BuildJudgeMessages(candidates, userText)` 调用不变,其余 body 构建与调用逻辑不变:

```go
func (j *defaultJudgeClient) Judge(judgeModel *store.Model, candidates []Candidate, userText string) (string, *model.Usage, error) {
	msgs := BuildJudgeMessages(candidates, userText)
	// ... 其余逻辑不变 ...
```

- [ ] **Step 4: 改 `internal/routing/judge_test.go`**

`TestBuildJudgeMessages` 用 `[]Candidate`:

```go
func TestBuildJudgeMessages(t *testing.T) {
	candidates := []Candidate{
		{Name: "deepseek-v4-flash", Description: "fast"},
		{Name: "gpt-4o-pro", Description: "smart"},
	}
	msgs := BuildJudgeMessages(candidates, "Write a haiku")
	assert.Equal(t, "system", msgs[0].Role)
	assert.Contains(t, msgs[0].Content, "模型路由器")
	assert.Equal(t, "user", msgs[1].Role)
	assert.Contains(t, msgs[1].Content, "deepseek-v4-flash")
	assert.Contains(t, msgs[1].Content, "gpt-4o-pro")
	assert.Contains(t, msgs[1].Content, "Write a haiku")
}
```

`TestParseJudgeOutput`、`TestTruncateUserText` 不变。移除文件中不再使用的 `"auto-router/internal/store"` import(若 `BuildJudgeMessages` 不再用 `store.Model`)。

- [ ] **Step 5: 改 `internal/routing/judge_client_test.go`**

把 `[]store.Model{{Name: "gpt-4o"}}` 改为 `[]Candidate{{Name: "gpt-4o"}}`:

```go
	out, _, err := jc.Judge(judgeModel, []Candidate{{Name: "gpt-4o"}}, "hi")
```

- [ ] **Step 6: 重写 `internal/routing/engine_test.go`**

```go
package routing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/model"
	"auto-router/internal/store"
)

type fakeStore struct {
	judge    *store.Model
	defGroup *uint
	groups   []store.ModelGroup
	chains   map[uint][]store.Model
	byName   map[string]*store.ModelGroup
}

func (f *fakeStore) GetJudgeModel() (*store.Model, error)            { return f.judge, nil }
func (f *fakeStore) GetRoutingConfig() (*store.RoutingConfig, error) { return &store.RoutingConfig{DefaultGroupID: f.defGroup, JudgeMaxInputChars: 1000}, nil }
func (f *fakeStore) ListEnabledModelGroups() ([]store.ModelGroup, error) { return f.groups, nil }
func (f *fakeStore) GetModelGroup(id uint) (*store.ModelGroup, error) {
	for _, g := range f.groups {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeStore) GetModelGroupByName(n string) (*store.ModelGroup, error) {
	if g, ok := f.byName[n]; ok {
		return g, nil
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeStore) GetGroupChain(groupID uint) ([]store.Model, error) { return f.chains[groupID], nil }

type fakeJudge struct {
	out string
	err error
}

func (fj *fakeJudge) Judge(judgeModel *store.Model, candidates []Candidate, userText string) (string, *model.Usage, error) {
	return fj.out, nil, fj.err
}

func puint(v uint) *uint { return &v }

func newEngine() (*Engine, *fakeStore, *fakeJudge) {
	m := store.Model{ID: 1, Name: "gpt-4o", Enabled: true}
	g := store.ModelGroup{ID: 7, Name: "deepseek-v4-flash", Enabled: true}
	fs := &fakeStore{
		groups: []store.ModelGroup{g},
		byName: map[string]*store.ModelGroup{"deepseek-v4-flash": &g},
		chains: map[uint][]store.Model{7: {m}},
	}
	fj := &fakeJudge{out: "deepseek-v4-flash"}
	return New(fs, fj), fs, fj
}

func TestRouteOverride(t *testing.T) {
	e, _, _ := newEngine()
	dec, err := e.Route(&model.ChatRequest{Override: "deepseek-v4-flash"})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "override", dec.Reason)
	assert.Len(t, dec.Models, 1)
}

func TestRouteOverrideUnknownQueue(t *testing.T) {
	e, _, _ := newEngine()
	_, err := e.Route(&model.ChatRequest{Override: "no-such-queue"})
	assert.Error(t, err)
}

func TestRouteJudge(t *testing.T) {
	e, fs, _ := newEngine()
	fs.judge = &store.Model{ID: 9, Name: "judge-mini"}
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "judge", dec.Reason)
}

func TestRouteFallbackOnBadJudge(t *testing.T) {
	e, fs, fj := newEngine()
	fs.judge = &store.Model{ID: 9, Name: "judge-mini"}
	fj.out = "nonexistent"
	fs.defGroup = puint(7)
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "fallback", dec.Reason)
}
```

- [ ] **Step 7: 改 `internal/server/server.go` 的 lazyJudge 签名**

```go
func (l *lazyJudge) Judge(judgeModel *store.Model, candidates []routing.Candidate, userText string) (string, *model.Usage, error) {
	prov, err := l.st.GetProvider(judgeModel.ProviderID)
	if err != nil {
		return "", nil, err
	}
	apiKey, _ := store.Decrypt(l.key, prov.APIKey)
	return routing.NewJudgeClient(l.disp, prov.BaseURL, apiKey, prov.Protocol).Judge(judgeModel, candidates, userText)
}
```

- [ ] **Step 8: 改 `internal/server/apptest_test.go`**

两个 `newTestApp*`:目标模型放进队列,`DefaultModelID` 改为 `DefaultGroupID`。以 `newTestApp` 为例,把原 `target` 创建后那段替换为:

```go
	target := &store.Model{Name: "gpt-4o", DisplayName: "GPT4o", ProviderID: prov.ID, Enabled: true}
	if err := st.CreateModel(target); err != nil {
		t.Fatal(err)
	}
	grp := &store.ModelGroup{Name: "deepseek-v4-flash", DisplayName: "DSV4", Enabled: true}
	if err := st.CreateModelGroup(grp); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGroupItems(grp.ID, []uint{target.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateRoutingConfig(&store.RoutingConfig{
		ID:                 1,
		JudgeModelID:       &judge.ID,
		DefaultGroupID:     &grp.ID,
		JudgeMaxInputChars: 2000,
	}); err != nil {
		t.Fatal(err)
	}
```

`newTestAppWithProtocol` 同样改造:把 `claude-3` 放进一个队列(如 name `claude-queue`),`DefaultGroupID` 指向它。

- [ ] **Step 9: 改 `internal/server/gateway_test.go` 的 mock**

`startMockUpstream` 中 judge 调用返回队列名 `deepseek-v4-flash`:

```go
		content := "hello"
		if m, _ := body["model"].(string); m == "judge-mini" {
			content = "deepseek-v4-flash"
		}
```

`TestGatewayRequestedVsRoutedModel` 的断言改:

```go
		assert.Equal(t, "deepseek-v4-flash", logs[0].RoutedModel)
```

- [ ] **Step 10: 改 `internal/server/integration_test.go`**

`TestEndToEndOverrideSkipsRouting` 显式指定的是模型名 `gpt-4o`,新规则下 override 必须是队列名,改为队列名 `deepseek-v4-flash`:

```go
	body := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`
```

该用例断言 `reason="override"` 有一条日志,改后仍成立(命中默认 seed 的队列)。`assert.Len(t, logs, 1)` 不变。

`startStreamingUpstream` 的 judge 分支返回的内容由模型名 `gpt-4o` 改为队列名 `deepseek-v4-flash`,使 judge 正常命中而非回退:

```go
		if m, _ := body["model"].(string); m == "judge-mini" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"deepseek-v4-flash"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
			return
		}
```

- [ ] **Step 11: 编译 + 测试**

Run: `go build ./... && go test ./...`
Expected: 全部 PASS(含 cross_protocol / integration / gateway 用例)。此时网关仍用 `dec.Model`(链首)单次调用,无转移,但路由已走队列。

- [ ] **Step 12: 提交**

```bash
git add internal/routing/ internal/server/server.go internal/server/apptest_test.go internal/server/gateway_test.go
git commit -m "feat(routing): groups-only routing with ordered model chain"
```

---

## Task 4: 网关 - 失败转移循环 + /v1/models 只列队列 + 日志回填

**Files:**
- Modify: `internal/server/gateway.go`(handleChat 循环、streamResponse 循环、handleListModels、writeLog)
- Modify: `internal/server/gateway_test.go`(新增转移用例)

- [ ] **Step 1: 重写 `handleChat` 执行段为链式循环**

把 `handleChat` 中从 `requestedModel := req.Model` 到函数末尾替换为:

```go
	requestedModel := req.Model

	if req.Stream {
		a.streamResponse(c, dec, req, requestedModel, start)
		return
	}

	// 非流式:遍历链,任意失败转下一个,单模型仍用其 Provider 的 RetryMax 重试。
	status := http.StatusOK
	errMsg := ""
	var resp *model.ChatResponse
	var retryCount int
	var lastErr error
	for i, m := range dec.Models {
		prov, perr := a.Store.GetProvider(m.ProviderID)
		if perr != nil {
			lastErr = fmt.Errorf("provider not found for model %s", m.Name)
			continue
		}
		apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)
		req.Model = m.Name
		var body map[string]any
		if prov.Protocol == "claude" {
			body, _ = claude.BuildUpstreamRequest(req)
		} else {
			body, _ = openai.BuildUpstreamRequest(req)
		}
		resp, retryCount, err = a.Dispatcher.CallWithRetry(c.Request.Context(), prov.BaseURL, apiKey, prov.Protocol, body, prov.RetryMax, prov.RetryBackoffMs)
		if err == nil {
			dec.ServedModel = m.Name
			dec.FailoverCount = i
			break
		}
		lastErr = err
	}
	if resp == nil && lastErr != nil {
		status = http.StatusBadGateway
		errMsg = lastErr.Error()
		writeGatewayError(c, status, clientFmt, lastErr.Error(), "upstream_error")
	} else {
		var b []byte
		if req.ClientFmt == "claude" {
			b, _ = claude.EncodeResponseToClient(resp)
		} else {
			b, _ = openai.EncodeResponseToClient(resp)
		}
		c.Data(http.StatusOK, "application/json", b)
	}
	var usage *model.Usage
	if resp != nil {
		usage = &resp.Usage
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount, usage)
```

补充 `"fmt"` import(若未引入)。

- [ ] **Step 2: 重写 `streamResponse` 为链式循环**

签名改为接收 `dec *routing.Decision`,整体替换为:

```go
func (a *App) streamResponse(c *gin.Context, dec *routing.Decision, req *model.ChatRequest, requestedModel string, start time.Time) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, _ := c.Writer.(http.Flusher)
	status := http.StatusOK
	errMsg := ""

	var usage *model.Usage
	retryCount := 0
	var lastErr error
	succeeded := false
	for i, m := range dec.Models {
		prov, perr := a.Store.GetProvider(m.ProviderID)
		if perr != nil {
			lastErr = fmt.Errorf("provider not found for model %s", m.Name)
			continue
		}
		apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)
		req.Model = m.Name
		var body map[string]any
		if prov.Protocol == "claude" {
			body, _ = claude.BuildUpstreamRequest(req)
		} else {
			body, _ = openai.BuildUpstreamRequest(req)
		}
		var enc chunkEncoder
		if req.ClientFmt == "claude" {
			enc = &claudeChunkEncoder{enc: claude.NewStreamEncoder(m.Name)}
		} else {
			enc = openaiChunkEncoder{}
		}
		started := false
		rc, streamErr := a.Dispatcher.CallStreamWithRetry(prov.BaseURL, apiKey, prov.Protocol, body, prov.RetryMax, prov.RetryBackoffMs, func(ch *model.Chunk) error {
			started = true
			if ch != nil && ch.Usage != nil {
				usage = ch.Usage
			}
			if ch == nil {
				c.Writer.Write(enc.Finish())
				flusher.Flush()
				return nil
			}
			c.Writer.Write(enc.EncodeChunk(ch))
			flusher.Flush()
			return nil
		})
		retryCount = rc
		if streamErr == nil {
			dec.ServedModel = m.Name
			dec.FailoverCount = i
			succeeded = true
			break
		}
		lastErr = streamErr
		if started {
			// 已输出内容,不可转移(避免重复),立即结束
			break
		}
		// 首字节前失败,尝试下一个模型
	}
	if !succeeded && lastErr != nil {
		status = http.StatusBadGateway
		errMsg = lastErr.Error()
		writeGatewayError(c, status, req.ClientFmt, lastErr.Error(), "upstream_error")
	}
	a.writeLog(req, dec, requestedModel, status, time.Since(start), errMsg, retryCount, usage)
}
```

- [ ] **Step 3: 改 `handleListModels` 只列启用且链非空的队列**

```go
func (a *App) handleListModels(c *gin.Context) {
	gs, err := a.Store.ListEnabledModelGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	data := []gin.H{}
	for _, g := range gs {
		chain, err := a.Store.GetGroupChain(g.ID)
		if err != nil || len(chain) == 0 {
			continue
		}
		data = append(data, gin.H{"id": g.Name, "object": "model", "owned_by": "auto-router"})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}
```

- [ ] **Step 4: 改 `writeLog` 回填新字段**

在 `CreateLog` 调用里增加 `ServedModel`、`FailoverCount` 两个字段(其余不变):

```go
		RetryCount:            retryCount,
		ServedModel:           dec.ServedModel,
		FailoverCount:         dec.FailoverCount,
		PromptTokens:          prompt,
```

- [ ] **Step 5: 写失败转移测试**

在 `internal/server/gateway_test.go` 新增(顶部补 import:`"auto-router/internal/config"`、`"auto-router/internal/store"`,若未引入):

```go
func TestGatewayQueueFailoverNonStream(t *testing.T) {
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if m, _ := body["model"].(string); m == "judge-mini" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"model":"judge-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ignored"},"finish_reason":"stop"}],"usage":{"total_tokens":1}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":"boom"}`)
	}))
	t.Cleanup(failSrv.Close)
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"m2","choices":[{"index":0,"message":{"role":"assistant","content":"ok-from-m2"},"finish_reason":"stop"}],"usage":{"total_tokens":2}}`)
	}))
	t.Cleanup(okSrv.Close)

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	key := store.DeriveKey("test-seed")
	pfail := &store.Provider{Name: "fail", BaseURL: failSrv.URL, APIKey: store.Encrypt(key, "sk"), Protocol: "openai", Enabled: true}
	pok := &store.Provider{Name: "ok", BaseURL: okSrv.URL, APIKey: store.Encrypt(key, "sk"), Protocol: "openai", Enabled: true}
	assert.NoError(t, st.CreateProvider(pfail))
	assert.NoError(t, st.CreateProvider(pok))
	judge := &store.Model{Name: "judge-mini", DisplayName: "J", ProviderID: pfail.ID, Enabled: true}
	assert.NoError(t, st.CreateModel(judge))
	assert.NoError(t, st.SetJudgeModel(judge.ID))
	m1 := &store.Model{Name: "m1", DisplayName: "1", ProviderID: pfail.ID, Enabled: true}
	m2 := &store.Model{Name: "m2", DisplayName: "2", ProviderID: pok.ID, Enabled: true}
	assert.NoError(t, st.CreateModel(m1))
	assert.NoError(t, st.CreateModel(m2))
	g := &store.ModelGroup{Name: "q-failover", DisplayName: "Q", Enabled: true}
	assert.NoError(t, st.CreateModelGroup(g))
	assert.NoError(t, st.ReplaceGroupItems(g.ID, []uint{m1.ID, m2.ID}))

	app := NewApp(config.Config{}, st, key, "gw", "admin")
	body := `{"model":"q-failover","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer gw")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok-from-m2")
	logs, _, _ := st.ListLogs(1, 10, "override", "")
	if assert.Len(t, logs, 1) {
		assert.Equal(t, "m2", logs[0].ServedModel)
		assert.Equal(t, 1, logs[0].FailoverCount)
		assert.Equal(t, "q-failover", logs[0].RoutedModel)
	}
}
```

- [ ] **Step 6: 编译 + 测试**

Run: `go build ./... && go test ./internal/server/ -run TestGateway -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/server/gateway.go internal/server/gateway_test.go
git commit -m "feat(gateway): chain failover loop, list queues, served model logging"
```

---

## Task 5: Admin - 队列 CRUD + items 端点

**Files:**
- Modify: `internal/server/admin.go`(新增 handlers)
- Modify: `internal/server/server.go`(注册路由)
- Modify: `internal/server/admin_test.go`(新增用例)

- [ ] **Step 1: 在 `internal/server/admin.go` 增加 handlers**

```go
// ---- Model Groups (queues) ----

type groupInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

func (a *App) handleListGroups(c *gin.Context) {
	gs, err := a.Store.ListModelGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type groupWithCount struct {
		store.ModelGroup
		ItemCount int64 `json:"item_count"`
	}
	out := make([]groupWithCount, 0, len(gs))
	for _, g := range gs {
		var cnt int64
		a.Store.DB.Model(&store.ModelGroupItem{}).Where("group_id = ?", g.ID).Count(&cnt)
		out = append(out, groupWithCount{ModelGroup: g, ItemCount: cnt})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) handleCreateGroup(c *gin.Context) {
	var in groupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g := store.ModelGroup{Name: in.Name, DisplayName: in.DisplayName, Description: in.Description, Enabled: in.Enabled}
	if err := a.Store.CreateModelGroup(&g); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (a *App) handleUpdateGroup(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	g, err := a.Store.GetModelGroup(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var in groupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.Name = in.Name
	g.DisplayName = in.DisplayName
	g.Description = in.Description
	g.Enabled = in.Enabled
	if err := a.Store.UpdateModelGroup(g); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (a *App) handleDeleteGroup(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	rc, _ := a.Store.GetRoutingConfig()
	if rc.DefaultGroupID != nil && *rc.DefaultGroupID == uint(id) {
		c.JSON(http.StatusConflict, gin.H{"error": "in use as default"})
		return
	}
	if err := a.Store.DeleteModelGroup(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type groupItemOut struct {
	ID       uint         `json:"id"`
	GroupID  uint         `json:"group_id"`
	ModelID  uint         `json:"model_id"`
	Position int          `json:"position"`
	Model    *store.Model `json:"model"`
}

func (a *App) handleListGroupItems(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	items, err := a.Store.GetGroupItemsOrdered(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]groupItemOut, 0, len(items))
	for _, it := range items {
		m, _ := a.Store.GetModel(it.ModelID)
		out = append(out, groupItemOut{ID: it.ID, GroupID: it.GroupID, ModelID: it.ModelID, Position: it.Position, Model: m})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) handleReplaceGroupItems(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		Items []uint `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := a.Store.ReplaceGroupItems(uint(id), body.Items); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
```

- [ ] **Step 2: 在 `internal/server/server.go` 注册路由**

在 `authAdmin` 组内追加:

```go
	authAdmin.GET("/groups", app.handleListGroups)
	authAdmin.POST("/groups", app.handleCreateGroup)
	authAdmin.PUT("/groups/:id", app.handleUpdateGroup)
	authAdmin.DELETE("/groups/:id", app.handleDeleteGroup)
	authAdmin.GET("/groups/:id/items", app.handleListGroupItems)
	authAdmin.PUT("/groups/:id/items", app.handleReplaceGroupItems)
```

- [ ] **Step 3: 写测试 `internal/server/admin_test.go`**

新增(复用该文件既有的 `adminToken(t, app)` 登录辅助):

```go
func TestAdminGroupsCRUDAndItems(t *testing.T) {
	app := newTestApp(t, startMockUpstream(t))
	tok := adminToken(t, app)
	h := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf *bytes.Buffer
		if body != nil {
			b, _ := json.Marshal(body)
			buf = bytes.NewBuffer(b)
		} else {
			buf = bytes.NewBuffer(nil)
		}
		req := httptest.NewRequest(method, path, buf)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		app.Router.ServeHTTP(w, req)
		return w
	}
	w := h("POST", "/admin/groups", groupInput{Name: "q", DisplayName: "Q", Enabled: true})
	assert.Equal(t, http.StatusOK, w.Code)

	ms, _ := app.Store.ListModels()
	assert.NotEmpty(t, ms)
	w = h("PUT", "/admin/groups/1/items", map[string]any{"items": []uint{ms[0].ID}})
	assert.Equal(t, http.StatusOK, w.Code)

	w = h("GET", "/admin/groups/1/items", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "items")

	defID := uint(1)
	assert.NoError(t, app.Store.UpdateRoutingConfig(&store.RoutingConfig{ID: 1, DefaultGroupID: &defID, JudgeMaxInputChars: 1000}))
	w = h("DELETE", "/admin/groups/1", nil)
	assert.Equal(t, http.StatusConflict, w.Code)
}
```

- [ ] **Step 4: 编译 + 测试**

Run: `go build ./... && go test ./internal/server/ -run TestAdminGroups -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/server/admin.go internal/server/server.go internal/server/admin_test.go
git commit -m "feat(admin): model group CRUD and items endpoints"
```

---

## Task 6: Admin - 路由配置端点 default_group_id + 清理 DefaultModelID

**Files:**
- Modify: `internal/server/admin.go`(`handleGetRouting` / `handleUpdateRouting`)
- Modify: `internal/store/routing.go`(移除 deprecated `DefaultModelID`)
- Modify: 任何仍引用 `DefaultModelID` 的测试(此时应已无)

- [ ] **Step 1: 改 `handleGetRouting`**

返回字段 `default_model_id` 改为 `default_group_id`:

```go
	c.JSON(http.StatusOK, gin.H{
		"id":                  rc.ID,
		"judge_model_id":      rc.JudgeModelID,
		"default_group_id":    rc.DefaultGroupID,
		"judge_max_input_chars": rc.JudgeMaxInputChars,
		"gateway_token":       a.GatewayTokenValue(),
	})
```

- [ ] **Step 2: 改 `handleUpdateRouting`**

入参与构造改用 `default_group_id` / `DefaultGroupID`:

```go
	var body struct {
		JudgeModelID       *uint  `json:"judge_model_id"`
		DefaultGroupID     *uint  `json:"default_group_id"`
		JudgeMaxInputChars int    `json:"judge_max_input_chars"`
		GatewayToken       string `json:"gateway_token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rc := store.RoutingConfig{
		ID:                 1,
		JudgeModelID:       body.JudgeModelID,
		DefaultGroupID:     body.DefaultGroupID,
		JudgeMaxInputChars: body.JudgeMaxInputChars,
	}
```

成功响应同样改为 `default_group_id`(去掉 `default_model_id`)。

- [ ] **Step 3: 移除 `RoutingConfig.DefaultModelID`**

`internal/store/routing.go` 删除 `DefaultModelID *uint` 字段。

- [ ] **Step 4: 全局校验无残留**

Run: `go build ./...`
Expected: 编译通过。若报错,定位残留 `DefaultModelID` 引用并改为 `DefaultGroupID`(Task 3 已改 engine/engine_test/apptest;此处一般只剩 admin,已随 Step 1-2 处理)。

- [ ] **Step 5: 测试**

Run: `go build ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/server/admin.go internal/store/routing.go
git commit -m "feat(admin): routing config uses default_group_id; drop default_model_id"
```

---

## Task 7: 前端 - API 模块

**Files:**
- Create: `web/src/api/groups.ts`
- Modify: `web/src/api/routing.ts`(`default_model_id` -> `default_group_id`)

- [ ] **Step 1: 写 `web/src/api/groups.ts`**

```ts
import apiClient from './client'
import type { Model } from './models'

export interface ModelGroup {
  id: number
  name: string
  display_name: string
  description: string
  enabled: boolean
  item_count?: number
}

export interface GroupItem {
  id: number
  group_id: number
  model_id: number
  position: number
  model?: Model
}

export async function listGroups(): Promise<ModelGroup[]> {
  const { data } = await apiClient.get('/admin/groups')
  return data.data
}

export async function createGroup(g: Partial<ModelGroup>): Promise<ModelGroup> {
  const { data } = await apiClient.post('/admin/groups', g)
  return data
}

export async function updateGroup(id: number, g: Partial<ModelGroup>): Promise<ModelGroup> {
  const { data } = await apiClient.put(`/admin/groups/${id}`, g)
  return data
}

export async function deleteGroup(id: number): Promise<void> {
  await apiClient.delete(`/admin/groups/${id}`)
}

export async function listGroupItems(id: number): Promise<GroupItem[]> {
  const { data } = await apiClient.get(`/admin/groups/${id}/items`)
  return data.data
}

export async function replaceGroupItems(id: number, modelIds: number[]): Promise<void> {
  await apiClient.put(`/admin/groups/${id}/items`, { items: modelIds })
}
```

- [ ] **Step 2: 改 `web/src/api/routing.ts`**

```ts
export interface RoutingConfig {
  id: number
  judge_model_id: number | null
  default_group_id: number | null
  judge_max_input_chars: number
  gateway_token: string
}
```

- [ ] **Step 3: 提交**

```bash
git add web/src/api/groups.ts web/src/api/routing.ts
git commit -m "feat(web): add groups api module and routing default_group_id"
```

> 说明:此 Task 后 `Routing.tsx` 仍引用旧字段,会在 Task 9 一并修复并统一 `npm run build` 验证。

---

## Task 8: 前端 - 队列管理页

**Files:**
- Create: `web/src/pages/Queues.tsx`
- Modify: `web/src/components/Layout.tsx`(加菜单)
- Modify: `web/src/App.tsx`(加 `/queues` 路由)

- [ ] **Step 1: 写 `web/src/pages/Queues.tsx`**

```tsx
import { useState } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Space, Popconfirm, message, Empty, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  listGroups, createGroup, updateGroup, deleteGroup,
  listGroupItems, replaceGroupItems, type ModelGroup,
} from '../api/groups'
import { listModels, type Model } from '../api/models'

export default function Queues() {
  const qc = useQueryClient()
  const { data: groups, isLoading } = useQuery({ queryKey: ['groups'], queryFn: listGroups })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: listModels })

  const [editOpen, setEditOpen] = useState(false)
  const [editing, setEditing] = useState<ModelGroup | null>(null)
  const [form] = Form.useForm()

  const [memberOpen, setMemberOpen] = useState(false)
  const [memberGroupId, setMemberGroupId] = useState<number | null>(null)
  const [picked, setPicked] = useState<number[]>([])

  const createMut = useMutation({
    mutationFn: createGroup,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('创建成功') },
    onError: () => message.error('创建失败'),
  })
  const updateMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ModelGroup> }) => updateGroup(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('更新成功') },
    onError: () => message.error('更新失败'),
  })
  const deleteMut = useMutation({
    mutationFn: deleteGroup,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('已删除') },
    onError: (err: any) => err?.response?.status === 409 ? message.warning('默认队列无法删除') : message.error('删除失败'),
  })
  const replaceMut = useMutation({
    mutationFn: (p: { id: number; ids: number[] }) => replaceGroupItems(p.id, p.ids),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('已保存成员') },
    onError: () => message.error('保存失败'),
  })

  const openCreate = () => { setEditing(null); form.resetFields(); form.setFieldsValue({ enabled: true }); setEditOpen(true) }
  const openEdit = (g: ModelGroup) => { setEditing(g); form.setFieldsValue(g); setEditOpen(true) }
  const submit = async () => {
    const vals = await form.validateFields()
    try {
      if (editing) await updateMut.mutateAsync({ id: editing.id, data: vals })
      else await createMut.mutateAsync(vals)
      setEditOpen(false)
    } catch { /* msg shown by mutation */ }
  }

  const openMembers = async (g: ModelGroup) => {
    setMemberGroupId(g.id)
    const items = await listGroupItems(g.id)
    setPicked(items.map((i) => i.model_id))
    setMemberOpen(true)
  }
  const saveMembers = async () => {
    if (memberGroupId === null) return
    try { await replaceMut.mutateAsync({ id: memberGroupId, ids: picked }); setMemberOpen(false) } catch { /* msg */ }
  }

  const enabledModels = (models ?? []).filter((m) => m.enabled)
  const pickedModels = picked.map((id) => enabledModels.find((m) => m.id === id)).filter(Boolean) as Model[]
  const available = enabledModels.filter((m) => !picked.includes(m.id))

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '展示名', dataIndex: 'display_name', key: 'display_name' },
    { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
    { title: '模型数', dataIndex: 'item_count', key: 'item_count', width: 90 },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled', width: 80,
      render: (_: boolean, r: ModelGroup) => (
        <Switch checked={r.enabled} onChange={(v) => updateMut.mutate({ id: r.id, data: { ...r, enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ModelGroup) => (
        <Space>
          <Button size="small" onClick={() => openMembers(r)}>管理成员</Button>
          <Button size="small" onClick={() => openEdit(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const move = (idx: number, dir: -1 | 1) => {
    const next = [...picked]
    const j = idx + dir
    if (j < 0 || j >= next.length) return
    ;[next[idx], next[j]] = [next[j], next[idx]]
    setPicked(next)
  }

  return (
    <div>
      <div className="page-title">模型队列</div>
      <div className="page-subtitle">聚合多个模型为具名队列,按序失败转移;队列是对外唯一可路由目标</div>
      <div style={{ marginBottom: 12 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加队列</Button>
      </div>
      <Table columns={columns} dataSource={groups} rowKey="id" loading={isLoading} locale={{ emptyText: <Empty description="暂无队列" /> }} />

      <Modal title={editing ? '编辑队列' : '添加队列'} open={editOpen} onOk={submit} onCancel={() => setEditOpen(false)} confirmLoading={createMut.isPending || updateMut.isPending}>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如 deepseek-v4-flash" />
          </Form.Item>
          <Form.Item name="display_name" label="展示名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述" tooltip="给判定模型看的能力描述">
            <Input.TextArea />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Modal>

      <Modal title="管理队列成员" open={memberOpen} onOk={saveMembers} onCancel={() => setMemberOpen(false)} width={560} confirmLoading={replaceMut.isPending}>
        <div style={{ display: 'flex', gap: 16 }}>
          <div style={{ flex: 1 }}>
            <div style={{ marginBottom: 8, fontWeight: 600 }}>可选模型</div>
            <Select
              style={{ width: '100%' }}
              placeholder="选择模型添加"
              value={undefined}
              options={available.map((m) => ({ value: m.id, label: m.name }))}
              onChange={(id) => { if (!picked.includes(id)) setPicked([...picked, id]) }}
            />
          </div>
          <div style={{ flex: 1 }}>
            <div style={{ marginBottom: 8, fontWeight: 600 }}>队列成员(顺序即请求顺序)</div>
            {pickedModels.length === 0 ? (
              <Empty description="未添加成员" />
            ) : pickedModels.map((m, idx) => (
              <div key={m.id} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '4px 0' }}>
                <Tag>{idx + 1}</Tag>
                <span style={{ flex: 1 }}>{m.name}</span>
                <Space size="small">
                  <Button size="small" disabled={idx === 0} onClick={() => move(idx, -1)}>↑</Button>
                  <Button size="small" disabled={idx === pickedModels.length - 1} onClick={() => move(idx, 1)}>↓</Button>
                  <Button size="small" danger onClick={() => setPicked(picked.filter((x) => x !== m.id))}>移除</Button>
                </Space>
              </div>
            ))}
          </div>
        </div>
      </Modal>
    </div>
  )
}
```

- [ ] **Step 2: `web/src/components/Layout.tsx` 加菜单**

在 `menuItems` 中 `模型管理` 之后插入,并在 import 加 `UnorderedListOutlined`:

```tsx
  { key: '/queues', icon: <UnorderedListOutlined />, label: '模型队列' },
```

- [ ] **Step 3: `web/src/App.tsx` 加路由**

加入 `<Route path="/queues" element={<Queues />} />` 并 import(参照现有 `/sources` 路由写法)。

- [ ] **Step 4: 提交**

```bash
git add web/src/pages/Queues.tsx web/src/components/Layout.tsx web/src/App.tsx
git commit -m "feat(web): add model queue management page"
```

---

## Task 9: 前端 - 路由配置页默认队列选择器

**Files:**
- Modify: `web/src/pages/Routing.tsx`(默认兜底选择器从模型改为队列)

- [ ] **Step 1: 改 `web/src/pages/Routing.tsx`**

- 把"默认兜底"相关的 `default_model_id` 状态/字段名改为 `default_group_id`。
- 该下拉数据源从"启用模型列表"改为"启用队列列表":用 `listGroups()` 过滤 `enabled`,`value` 用 `g.id`,`label` 用 `g.name`。
- 提交时 `updateRoutingConfig` 的 payload 字段改为 `default_group_id`。
- judge 模型选择器保持不变(仍选单个模型)。

实现要点(伪代码,按现有 Routing.tsx 结构替换对应片段):

```tsx
const { data: groups } = useQuery({ queryKey: ['groups'], queryFn: listGroups })
const enabledGroups = (groups ?? []).filter((g) => g.enabled)

// 默认兜底 Select
<Select
  allowClear
  placeholder="选择默认兜底队列"
  value={config?.default_group_id ?? undefined}
  onChange={(v) => setConfig({ ...config, default_group_id: v ?? null })}
  options={enabledGroups.map((g) => ({ value: g.id, label: g.name }))}
/>

// 保存
updateRoutingConfig({
  judge_model_id: config.judge_model_id,
  default_group_id: config.default_group_id,
  judge_max_input_chars: config.judge_max_input_chars,
})
```

并在文件顶部 import `import { listGroups } from '../api/groups'`。

- [ ] **Step 2: 前端构建验证**

Run: `cd web && npm run build`
Expected: 构建通过

- [ ] **Step 3: 提交**

```bash
git add web/src/pages/Routing.tsx
git commit -m "feat(web): routing default fallback selector uses queues"
```

---

## Task 10: 端到端验证

- [ ] **Step 1: 后端全量测试**

Run: `go build ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 2: 前端构建**

Run: `cd web && npm run build`
Expected: 构建通过

- [ ] **Step 3: 手动验证(可选)**

启动服务,登录后台:添加 Provider/模型 -> 在"模型队列"页创建队列并按序加入模型 -> 在"路由配置"设默认队列 -> 客户端 `POST /v1/chat/completions` 用 `model=队列名` 验证转移(可临时让首模型上游返回 500 观察日志 `served_model` 与 `failover_count`)。

- [ ] **Step 4: 更新记忆文档**

按 AGENTS.md 5.5,更新 `MEMORY.md`(若存在)记录:队列是对外唯一可路由目标、judge 候选只含队列、默认兜底为 DefaultGroupID、显式非队列名报错、日志新增 served_model/failover_count。
