# 判定队列改造 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将智能路由的判定由"单模型"升级为"判定队列"——`JudgeModelID` 改为 `JudgeGroupID` 指向 `ModelGroup`，判定按队列内模型顺序失败转移，全失败再回退默认兜底队列；同时移除旧的单模型判定设施（`is_judge` 字段、`SetJudgeModel`/`GetJudgeModel`/`IsModelReferenced`、`POST /admin/models/:id/judge` 端点）。

**Architecture:** 失败转移循环封装在 `JudgeClient` 内（方案 A）：`JudgeClient.Judge` 接收整条判定链 `[]*store.Model`，返回 `(raw, servedModel, usage, err)`；`lazyJudge` 内部逐模型解析 provider 并重试。引擎 `Route` 只调一次 `Judge`，err 即走兜底。旧配置通过 `migrateLegacyJudge` 在 `store.Open` 中以 `models.is_judge` 为首选源自动迁移为判定队列并丢弃遗留列。

**Tech Stack:** Go 1.x + GORM + Gin + SQLite/MySQL；React + TypeScript + Ant Design + TanStack Query（前端在 `web/`）。测试用 `github.com/stretchr/testify/assert`。

**Spec（唯一事实源）：** `docs/superpowers/specs/2026-07-31-judge-queue-design.md`

**重要约束（来自 AGENTS.md）：** 最小干预、全局视角、Conventional Commits、每步 `go build`/`go vet`/`go test` 验证、同步 `MEMORY.md` 与 `project_memory.md`（强制）。PowerShell 不支持 heredoc，`git commit` 用多个 `-m`。禁止修改用户手动改过的代码。

---

## File Structure

| 文件 | 操作 | 责任 |
|------|------|------|
| `internal/store/routing.go` | 修改 | `RoutingConfig.JudgeModelID`→`JudgeGroupID`；`UpdateRoutingConfig` 删 `is_judge` 镜像 |
| `internal/store/models.go` | 修改 | 删 `Model.IsJudge` 字段、`SetJudgeModel`/`GetJudgeModel`/`IsModelReferenced` |
| `internal/store/store.go` | 修改 | 新增 `migrateLegacyJudge`，`Open` 中调用 |
| `internal/store/migrate_judge_test.go` | 新建 | 迁移幂等与回退源测试 |
| `internal/routing/engine.go` | 修改 | `StoreDeps` 去 `GetJudgeModel`；`JudgeClient` 接口改链式；`Route` 判定段改解析队列 + 排除判定队列候选 |
| `internal/routing/judge_client.go` | 修改 | `defaultJudgeClient` 降级为内部辅助，移除接口断言 |
| `internal/routing/engine_test.go` | 修改 | `fakeStore`/`fakeJudge` 适配链式接口 |
| `internal/server/server.go` | 修改 | 移除 `/models/:id/judge` 路由；`lazyJudge.Judge` 改链式失败转移 |
| `internal/server/admin.go` | 修改 | 删 `handleSetJudge`；routing 端点字段重命名；`handleDeleteModel` 去 `IsModelReferenced` |
| `internal/server/apptest_test.go` | 修改 | `newTestApp`/`newTestAppWithProtocol` 改为建判定队列 + `JudgeGroupID` |
| `internal/server/gateway_test.go` | 修改 | `TestGatewayQueueFailoverNonStream` 去掉 `SetJudgeModel` |
| `internal/server/gateway.go` | 无改动 | `dec.JudgeModel` 读取不变 |
| `web/src/api/routing.ts` | 修改 | `judge_model_id`→`judge_group_id` |
| `web/src/api/models.ts` | 修改 | 删 `is_judge` 字段、`setJudgeModel()` |
| `web/src/pages/Routing.tsx` | 修改 | 判定选择器改为队列下拉；移除 `modelOptions` |
| `web/src/pages/Sources.tsx` | 修改 | 删判定列与 `judgeModelId` 派生 |
| `README.md` | 修改 | 步骤 3 与功能特性文案 |
| `MEMORY.md` / `project_memory.md` | 修改 | 同步记忆（强制） |

**跨包编译说明：** Task 1 涉及 `store` + `routing` + `server` 三包的签名联动变更，中间步骤无法单独通过 `go build`，故 Task 1 各编辑步骤连续完成后再统一构建测试并提交。Task 2/3/4 各自独立可验证。

---

## Task 1: 后端判定队列链式改造

**Files:**
- Modify: `internal/store/routing.go`
- Modify: `internal/store/models.go`
- Modify: `internal/routing/engine.go`
- Modify: `internal/routing/judge_client.go`
- Modify: `internal/routing/engine_test.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/admin.go`
- Modify: `internal/server/apptest_test.go`
- Modify: `internal/server/gateway_test.go`

- [ ] **Step 1: 改 `internal/store/routing.go` — 字段重命名 + UpdateRoutingConfig 简化**

将整个文件替换为：

```go
package store

import "gorm.io/gorm"

type RoutingConfig struct {
	ID                 uint  `gorm:"primaryKey" json:"id"`
	JudgeGroupID       *uint `json:"judge_group_id"`        // 判定队列，指向 ModelGroup
	DefaultGroupID     *uint `json:"default_group_id"`      // default fallback queue
	JudgeMaxInputChars int   `gorm:"default:2000" json:"judge_max_input_chars"`
}

func (s *Store) GetRoutingConfig() (*RoutingConfig, error) {
	var rc RoutingConfig
	if err := s.DB.First(&rc, 1).Error; err != nil {
		return nil, err
	}
	return &rc, nil
}

// UpdateRoutingConfig saves the routing config singleton. 判定完全由
// judge_group_id 驱动；旧 is_judge 镜像逻辑已随判定队列化移除。
func (s *Store) UpdateRoutingConfig(rc *RoutingConfig) error {
	rc.ID = 1
	return s.DB.Save(rc).Error
}
```

- [ ] **Step 2: 改 `internal/store/models.go` — 删 IsJudge 字段与三个方法**

删除 `Model.IsJudge` 字段行，删除 `IsModelReferenced`、`SetJudgeModel`、`GetJudgeModel` 三个方法。同时删除因删除方法而不再需要的 的 import（`errors`、`gorm` 若仍被 `ListModels` 等用到则保留——`gorm.io/gorm` 仍被 `*gorm.DB` 用到，保留；`errors` 仅 `GetJudgeModel` 用，删除）。

替换后的完整文件：

```go
package store

import (
	"time"

	"gorm.io/gorm"
)

type Model struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	ProviderID  uint      `gorm:"not null" json:"provider_id"`
	Description string    `json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) ListModels() ([]Model, error) {
	var ms []Model
	err := s.DB.Order("id desc").Find(&ms).Error
	return ms, err
}

func (s *Store) ListEnabledModels() ([]Model, error) {
	var ms []Model
	err := s.DB.Where("enabled = ?", true).Find(&ms).Error
	return ms, err
}

func (s *Store) GetModel(id uint) (*Model, error) {
	var m Model
	if err := s.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) CreateModel(m *Model) error {
	return s.DB.Create(m).Error
}

func (s *Store) UpdateModel(m *Model) error {
	return s.DB.Save(m).Error
}

func (s *Store) DeleteModel(id uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("model_id = ?", id).Delete(&ModelGroupItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Model{}, id).Error
	})
}
```

- [ ] **Step 3: 改 `internal/routing/engine.go` — StoreDeps / JudgeClient / Route**

修改点：
1. `StoreDeps` 接口删除 `GetJudgeModel() (*store.Model, error)` 一行。
2. `JudgeClient` 接口签名改为链式四返回值。
3. `Route` 的 `// 2. Judge` 段改为解析判定队列链 + 排除判定队列自身 + 调用新 `Judge`。

将 `engine.go` 中 `StoreDeps` 接口与 `JudgeClient` 接口替换为：

```go
// StoreDeps is the subset of *store.Store the engine needs.
type StoreDeps interface {
	GetRoutingConfig() (*store.RoutingConfig, error)
	ListEnabledModelGroups() ([]store.ModelGroup, error)
	GetModelGroup(id uint) (*store.ModelGroup, error)
	GetModelGroupByName(name string) (*store.ModelGroup, error)
	GetGroupChain(groupID uint) ([]store.Model, error)
}

// Compile-time guarantee that *store.Store satisfies StoreDeps.
var _ StoreDeps = (*store.Store)(nil)

// JudgeClient invokes the judge queue to pick a queue name.
// Judge receives the ordered judge chain and returns:
//   - raw:       the judge model's textual output
//   - servedModel: the name of the judge model that actually succeeded
//                  (after failover within the chain)
//   - usage:     token usage of the successful judge call (nil on failure)
//   - err:       non-nil when the whole judge chain is exhausted
type JudgeClient interface {
	Judge(chain []*store.Model, candidates []Candidate, userText string) (raw string, servedModel string, usage *model.Usage, err error)
}
```

将 `Route` 方法中从 `// 2. Judge:` 注释起、到 `// 3. Fallback:` 注释前的整段替换为：

```go
	// 2. Judge: candidates are only enabled queues with non-empty chains,
	// excluding the judge queue itself (it is for judging, not for serving).
	rc, err := e.Store.GetRoutingConfig()
	if err != nil {
		return nil, fmt.Errorf("get routing config: %w", err)
	}
	judgeName := ""
	judgeRaw := ""
	var judgeUsage *model.Usage
	var judgeLatency time.Duration

	var judgeChain []*store.Model
	if rc.JudgeGroupID != nil {
		if g, gerr := e.Store.GetModelGroup(*rc.JudgeGroupID); gerr == nil && g != nil && g.Enabled {
			if ch, cerr := e.Store.GetGroupChain(g.ID); cerr == nil && len(ch) > 0 {
				judgeChain = toPtrChain(ch)
			}
		}
	}

	if len(judgeChain) > 0 {
		groups, _ := e.Store.ListEnabledModelGroups()
		cands := make([]Candidate, 0, len(groups))
		known := make([]string, 0, len(groups))
		for _, g := range groups {
			if rc.JudgeGroupID != nil && g.ID == *rc.JudgeGroupID {
				continue // exclude the judge queue itself
			}
			ch, err := e.Store.GetGroupChain(g.ID)
			if err != nil || len(ch) == 0 {
				continue
			}
			cands = append(cands, Candidate{Name: g.Name})
			known = append(known, g.Name)
		}
		userText := TruncateUserText(req.LastUserMessage(), rc.JudgeMaxInputChars)
		jStart := time.Now()
		raw, servedName, usage, jerr := e.Judge.Judge(judgeChain, cands, userText)
		judgeLatency = time.Since(jStart)
		judgeUsage = usage
		judgeName = servedName
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
					return &Decision{ModelName: picked, Model: chain[0], Models: chain, Reason: "judge", JudgeRaw: raw, JudgeModel: servedName, JudgeUsage: judgeUsage, JudgeLatency: judgeLatency}, nil
				}
			}
			log.Printf("[WARN] judge output unparseable: %q", raw)
			judgeRaw = raw
		}
	}
```

`// 3. Fallback:` 段保持不变（仍引用 `judgeRaw`/`judgeName`/`judgeUsage`/`judgeLatency`，这些变量在上方已声明）。

- [ ] **Step 4: 改 `internal/routing/judge_client.go` — defaultJudgeClient 降级**

仅删除第 23-24 行的编译期接口断言：

```go
// Compile-time guarantee that *defaultJudgeClient satisfies JudgeClient.
var _ JudgeClient = (*defaultJudgeClient)(nil)
```

`defaultJudgeClient.Judge` 单模型签名保持不变，`NewJudgeClient` 构造器保持不变。删除后该类型不再实现 `JudgeClient`（接口已改为链式），降级为 `lazyJudge` 内部调用的辅助。

- [ ] **Step 5: 改 `internal/routing/engine_test.go` — fakeStore / fakeJudge / 用例适配**

将整个文件替换为：

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
	judgeGroup *uint
	defGroup   *uint
	groups     []store.ModelGroup
	chains     map[uint][]store.Model
	byName     map[string]*store.ModelGroup
}

func (f *fakeStore) GetRoutingConfig() (*store.RoutingConfig, error) {
	return &store.RoutingConfig{JudgeGroupID: f.judgeGroup, DefaultGroupID: f.defGroup, JudgeMaxInputChars: 1000}, nil
}
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

func (fj *fakeJudge) Judge(chain []*store.Model, candidates []Candidate, userText string) (string, string, *model.Usage, error) {
	served := ""
	if len(chain) > 0 {
		served = chain[0].Name
	}
	return fj.out, served, nil, fj.err
}

func puint(v uint) *uint { return &v }

func newEngine() (*Engine, *fakeStore, *fakeJudge) {
	jm := store.Model{ID: 9, Name: "judge-mini", Enabled: true}
	m := store.Model{ID: 1, Name: "gpt-4o", Enabled: true}
	g := store.ModelGroup{ID: 7, Name: "deepseek-v4-flash", Enabled: true}
	jg := store.ModelGroup{ID: 8, Name: "judge", Enabled: true}
	fs := &fakeStore{
		groups: []store.ModelGroup{g, jg},
		byName: map[string]*store.ModelGroup{"deepseek-v4-flash": &g, "judge": &jg},
		chains: map[uint][]store.Model{7: {m}, 8: {jm}},
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
	fs.judgeGroup = puint(8) // judge queue
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "judge", dec.Reason)
	assert.Equal(t, "judge-mini", dec.JudgeModel)
}

func TestRouteFallbackOnBadJudge(t *testing.T) {
	e, fs, fj := newEngine()
	fs.judgeGroup = puint(8)
	fj.out = "nonexistent"
	fs.defGroup = puint(7)
	dec, err := e.Route(&model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	assert.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", dec.ModelName)
	assert.Equal(t, "fallback", dec.Reason)
}

func TestRouteOverrideMultiModelChainOrder(t *testing.T) {
	m1 := store.Model{ID: 1, Name: "m1", Enabled: true}
	m2 := store.Model{ID: 2, Name: "m2", Enabled: true}
	g := store.ModelGroup{ID: 8, Name: "multi-q", Enabled: true}
	fs := &fakeStore{
		groups: []store.ModelGroup{g},
		byName: map[string]*store.ModelGroup{"multi-q": &g},
		chains: map[uint][]store.Model{8: {m1, m2}},
	}
	e := New(fs, &fakeJudge{})
	dec, err := e.Route(&model.ChatRequest{Override: "multi-q"})
	assert.NoError(t, err)
	assert.Equal(t, "multi-q", dec.ModelName)
	assert.Len(t, dec.Models, 2)
	assert.Equal(t, "m1", dec.Models[0].Name)
	assert.Equal(t, "m2", dec.Models[1].Name)
	assert.Equal(t, "m1", dec.Model.Name) // chain head
}
```

- [ ] **Step 6: 改 `internal/server/server.go` — lazyJudge 链式 + 删路由注册**

6a. 在 `import` 块中加入 `"fmt"`（lazyJudge 失败转移需要）。修改后的 import 块：

```go
import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"auto-router/internal/config"
	"auto-router/internal/jwt"
	"auto-router/internal/model"
	"auto-router/internal/routing"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)
```

6b. 删除路由注册行（约第 115 行）：

```go
	authAdmin.POST("/models/:id/judge", app.handleSetJudge)
```

6c. 将 `lazyJudge.Judge` 方法替换为链式失败转移实现：

```go
// Judge 遍历判定队列链，逐模型解析 provider 并调用；首个成功（err==nil 且 raw != ""）
// 即返回。全部失败时返回 "judge queue exhausted" 错误，引擎据此走兜底。
func (l *lazyJudge) Judge(chain []*store.Model, candidates []routing.Candidate, userText string) (string, string, *model.Usage, error) {
	var lastErr error
	for _, jm := range chain {
		prov, err := l.st.GetProvider(jm.ProviderID)
		if err != nil {
			lastErr = err
			continue
		}
		apiKey, _ := store.Decrypt(l.key, prov.APIKey)
		raw, usage, err := routing.NewJudgeClient(l.disp, prov.BaseURL, apiKey, prov.Protocol, prov.ProxyURL).Judge(jm, candidates, userText)
		if err == nil && raw != "" {
			return raw, jm.Name, usage, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("judge queue exhausted")
	}
	return "", "", nil, fmt.Errorf("judge queue exhausted: %w", lastErr)
}
```

`var _ routing.JudgeClient = (*lazyJudge)(nil)` 断言保留（lazyJudge 仍是 JudgeClient 的实现）。

- [ ] **Step 7: 改 `internal/server/admin.go` — 删 handleSetJudge + routing 字段 + handleDeleteModel**

7a. 删除 `handleSetJudge` 整个函数（约第 373-380 行）：

```go
func (a *App) handleSetJudge(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := a.Store.SetJudgeModel(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
```

7b. 替换 `handleGetRouting`（字段重命名 + judge 校验）：

```go
func (a *App) handleGetRouting(c *gin.Context) {
	rc, err := a.Store.GetRoutingConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":                  rc.ID,
		"judge_group_id":      rc.JudgeGroupID,
		"default_group_id":    rc.DefaultGroupID,
		"judge_max_input_chars": rc.JudgeMaxInputChars,
		"gateway_token":       a.GatewayTokenValue(),
	})
}
```

7c. 替换 `handleUpdateRouting`（字段重命名 + judge 队列校验，复用默认队列校验模式）：

```go
func (a *App) handleUpdateRouting(c *gin.Context) {
	var body struct {
		JudgeGroupID       *uint  `json:"judge_group_id"`
		DefaultGroupID     *uint  `json:"default_group_id"`
		JudgeMaxInputChars int    `json:"judge_max_input_chars"`
		GatewayToken       string `json:"gateway_token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.JudgeGroupID != nil {
		g, err := a.Store.GetModelGroup(*body.JudgeGroupID)
		if err != nil || g == nil || !g.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "judge group not found or disabled"})
			return
		}
	}
	if body.DefaultGroupID != nil {
		g, err := a.Store.GetModelGroup(*body.DefaultGroupID)
		if err != nil || g == nil || !g.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "default group not found or disabled"})
			return
		}
	}
	rc := store.RoutingConfig{
		ID:                 1,
		JudgeGroupID:       body.JudgeGroupID,
		DefaultGroupID:     body.DefaultGroupID,
		JudgeMaxInputChars: body.JudgeMaxInputChars,
	}
	if err := a.Store.UpdateRoutingConfig(&rc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if body.GatewayToken != "" {
		if err := a.SetGatewayToken(body.GatewayToken); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"id":                  rc.ID,
		"judge_group_id":      rc.JudgeGroupID,
		"default_group_id":    rc.DefaultGroupID,
		"judge_max_input_chars": rc.JudgeMaxInputChars,
		"gateway_token":       a.GatewayTokenValue(),
	})
}
```

7d. 替换 `handleDeleteModel`（删除 `IsModelReferenced` 检查，直接级联删除）：

```go
func (a *App) handleDeleteModel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := a.Store.DeleteModel(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
```

- [ ] **Step 8: 改 `internal/server/apptest_test.go` — 建判定队列替换 SetJudgeModel**

将 `newTestApp` 与 `newTestAppWithProtocol` 中"创建 judge 模型 + SetJudgeModel + RoutingConfig.JudgeModelID"改为"创建 judge 模型 + 建判定队列 + ReplaceGroupItems + RoutingConfig.JudgeGroupID"。

`newTestApp` 中替换以下片段：

```go
	judge := &store.Model{Name: "judge-mini", ProviderID: prov.ID, Enabled: true}
	if err := st.CreateModel(judge); err != nil {
		t.Fatal(err)
	}
	if err := st.SetJudgeModel(judge.ID); err != nil {
		t.Fatal(err)
	}
```

为：

```go
	judge := &store.Model{Name: "judge-mini", ProviderID: prov.ID, Enabled: true}
	if err := st.CreateModel(judge); err != nil {
		t.Fatal(err)
	}
	judgeGrp := &store.ModelGroup{Name: "judge", Enabled: true}
	if err := st.CreateModelGroup(judgeGrp); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGroupItems(judgeGrp.ID, []uint{judge.ID}); err != nil {
		t.Fatal(err)
	}
```

并把同函数中的 `UpdateRoutingConfig` 调用里 `JudgeModelID: &judge.ID,` 改为 `JudgeGroupID: &judgeGrp.ID,`。

对 `newTestAppWithProtocol` 做同样改动（judge 模型名仍是 `judge-mini`，判定队列名也用 `judge`）。

- [ ] **Step 9: 改 `internal/server/gateway_test.go` — TestGatewayQueueFailoverNonStream 去掉 SetJudgeModel**

该测试是 override 场景（`model="q-failover"`），不调用判定。删除以下两行：

```go
	judge := &store.Model{Name: "judge-mini", ProviderID: pfail.ID, Enabled: true}
	assert.NoError(t, st.CreateModel(judge))
	assert.NoError(t, st.SetJudgeModel(judge.ID))
```

（`SetJudgeModel` 已被删除，保留会编译失败。该测试不依赖判定，删除 judge 模型创建不影响断言。）

- [ ] **Step 10: 构建与测试验证**

Run（在仓库根目录）：

```
go build ./...
go vet ./...
go test ./...
```

Expected: 全部通过。重点用例：
- `routing` 包：`TestRouteJudge`、`TestRouteFallbackOnBadJudge`、`TestRouteOverride*` PASS
- `server` 包：`TestGatewayNonStreamRoute`、`TestGatewayRequestedVsRoutedModel`、`TestGatewayQueueFailoverNonStream` PASS
- `routing/judge_client_test.go`：`TestJudgeClientCallsUpstream` PASS（单模型签名未动）

若 `go vet` 报 `judgeGroup`/`modelOptions` 等未使用变量，回到对应 step 修正。

- [ ] **Step 11: Commit**

```
git add internal/store/routing.go internal/store/models.go internal/routing/engine.go internal/routing/judge_client.go internal/routing/engine_test.go internal/server/server.go internal/server/admin.go internal/server/apptest_test.go internal/server/gateway_test.go
git commit -m "refactor: replace judge model with judge queue (chain failover)" -m "JudgeModelID -> JudgeGroupID; JudgeClient.Judge now takes the ordered judge chain and returns (raw, servedModel, usage, err); lazyJudge iterates the chain with per-model provider resolution. Removes IsJudge field, SetJudgeModel/GetJudgeModel/IsModelReferenced, and POST /admin/models/:id/judge. Engine excludes the judge queue from candidates."
```

---

## Task 2: store 遗留配置自动迁移

**Files:**
- Modify: `internal/store/store.go`
- Create: `internal/store/migrate_judge_test.go`

- [ ] **Step 1: 在 `internal/store/store.go` 新增 migrateLegacyJudge 并在 Open 中调用**

将整个文件替换为：

```go
package store

import (
	"fmt"

	"gorm.io/gorm"
)

type Store struct {
	DB *gorm.DB
}

// Open 使用给定 Dialer 打开数据库并完成通用初始化
// （AutoMigrate + seed routing_config 单例行 + 遗留判定配置迁移）。
// 驱动特定的初始化（PRAGMA、连接池等）由 Dialer 实现负责。
func Open(dialer Dialer, dsn string) (*Store, error) {
	db, err := dialer.Open(dsn)
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &RequestLog{}, &Setting{}, &ModelGroup{}, &ModelGroupItem{}); err != nil {
		return nil, err
	}
	// seed routing_config singleton row
	if err := db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1}).Error; err != nil {
		return nil, err
	}
	if err := migrateLegacyJudge(db); err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}

// migrateLegacyJudge 把旧"单模型判定"配置迁移为判定队列。
// 旧引擎真正的单一事实源是 models.is_judge=true AND enabled=true
// （routing_configs.judge_model_id 仅由 UpdateRoutingConfig 镜像写入，
// 且 POST /admin/models/:id/judge 只改 is_judge 不改 judge_model_id，
// 两者可能不一致），因此以 is_judge 为首选源，judge_model_id 为回退。
// 流程：
//  1. 检测 models.is_judge 与 routing_configs.judge_model_id 两列是否仍存在；
//     都已不存在 -> 直接返回（幂等）。
//  2. 确定旧判定模型 ID（is_judge 首选，judge_model_id 回退）。
//  3. 若有旧判定模型且当前 judge_group_id 为空：创建名为 judge 的队列
//     （重名则 judge-2/judge-3…），把旧判定模型加入为 Position 0，写回 judge_group_id。
//  4. DropColumn 丢弃两列。
//
// SQLite DROP COLUMN 需 SQLite >= 3.35（2021-03-12），项目所用现代驱动均满足。
func migrateLegacyJudge(db *gorm.DB) error {
	hasModelIsJudge := false
	hasRcJudgeModelID := false
	if cols, err := db.Migrator().ColumnTypes(&Model{}); err == nil {
		for _, c := range cols {
			if c.Name() == "is_judge" {
				hasModelIsJudge = true
			}
		}
	}
	if cols, err := db.Migrator().ColumnTypes(&RoutingConfig{}); err == nil {
		for _, c := range cols {
			if c.Name() == "judge_model_id" {
				hasRcJudgeModelID = true
			}
		}
	}
	if !hasModelIsJudge && !hasRcJudgeModelID {
		return nil // already migrated
	}

	// Determine legacy judge model id (prefer is_judge source of truth).
	var legacyJudgeID *uint
	if hasModelIsJudge {
		var id uint
		if err := db.Raw("SELECT id FROM models WHERE is_judge = ? AND enabled = ? LIMIT 1", true, true).Scan(&id).Error; err == nil && id > 0 {
			legacyJudgeID = &id
		}
	}
	if legacyJudgeID == nil && hasRcJudgeModelID {
		var id uint
		if err := db.Raw("SELECT judge_model_id FROM routing_configs WHERE id = 1").Scan(&id).Error; err == nil && id > 0 {
			legacyJudgeID = &id
		}
	}

	// Read current judge_group_id (column always exists after AutoMigrate).
	var rc RoutingConfig
	if err := db.First(&rc, 1).Error; err == nil {
		// rc.JudgeGroupID may be nil
	}

	if legacyJudgeID != nil && (rc.JudgeGroupID == nil) {
		name := "judge"
		for i := 2; ; i++ {
			var exist ModelGroup
			if err := db.Where("name = ?", name).First(&exist).Error; err != nil {
				break // name available (record not found)
			}
			name = fmt.Sprintf("judge-%d", i)
		}
		g := ModelGroup{Name: name, Remark: "migrated from legacy judge model", Enabled: true}
		if err := db.Create(&g).Error; err != nil {
			return err
		}
		if err := db.Create(&ModelGroupItem{GroupID: g.ID, ModelID: *legacyJudgeID, Position: 0}).Error; err != nil {
			return err
		}
		if err := db.Model(&RoutingConfig{}).Where("id = ?", 1).Update("judge_group_id", g.ID).Error; err != nil {
			return err
		}
	}

	// Drop legacy columns.
	if hasRcJudgeModelID && db.Migrator().HasColumn(&RoutingConfig{}, "judge_model_id") {
		if err := db.Migrator().DropColumn(&RoutingConfig{}, "judge_model_id"); err != nil {
			return err
		}
	}
	if hasModelIsJudge && db.Migrator().HasColumn(&Model{}, "is_judge") {
		if err := db.Migrator().DropColumn(&Model{}, "is_judge"); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: 新建 `internal/store/migrate_judge_test.go` — 迁移测试**

```go
package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openLegacyDB builds a DB with the PRE-migration schema (is_judge +
// judge_model_id columns) but WITHOUT running AutoMigrate on the new structs,
// so the legacy columns remain as the source of truth for migration.
func openLegacyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	// Minimal legacy schema.
	db.Exec("CREATE TABLE providers (id INTEGER PRIMARY KEY, name TEXT, base_url TEXT, api_key TEXT, protocol TEXT, enabled INTEGER, retry_max INTEGER, retry_backoff_ms INTEGER, proxy_url TEXT, created_at DATETIME)")
	db.Exec("CREATE TABLE models (id INTEGER PRIMARY KEY, name TEXT, provider_id INTEGER, description TEXT, enabled INTEGER, is_judge INTEGER, created_at DATETIME)")
	db.Exec("CREATE TABLE routing_configs (id INTEGER PRIMARY KEY, judge_model_id INTEGER, default_group_id INTEGER, judge_max_input_chars INTEGER, judge_group_id INTEGER)")
	db.Exec("CREATE TABLE model_groups (id INTEGER PRIMARY KEY, name TEXT UNIQUE, remark TEXT, enabled INTEGER, created_at DATETIME)")
	db.Exec("CREATE TABLE model_group_items (id INTEGER PRIMARY KEY, group_id INTEGER, model_id INTEGER, position INTEGER)")
	db.Exec("CREATE TABLE request_logs (id INTEGER PRIMARY KEY)")
	db.Exec("CREATE TABLE settings (id INTEGER PRIMARY KEY, key TEXT, value TEXT)")
	return db
}

func TestMigrateLegacyJudgeFromIsJudge(t *testing.T) {
	db := openLegacyDB(t)
	db.Exec("INSERT INTO providers (id, name, enabled, created_at) VALUES (1, 'p', 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (1, 'judge-mini', 1, 1, 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (2, 'gpt-4o', 1, 1, 0, datetime())")
	db.Exec("INSERT INTO routing_configs (id, judge_model_id, judge_group_id) VALUES (1, 2, NULL)")

	err := migrateLegacyJudge(db)
	assert.NoError(t, err)

	// Judge queue created with the legacy judge model.
	var g ModelGroup
	assert.NoError(t, db.Where("name = ?", "judge").First(&g).Error)
	assert.Equal(t, "migrated from legacy judge model", g.Remark)

	// judge_group_id written.
	var rc RoutingConfig
	assert.NoError(t, db.First(&rc, 1).Error)
	if assert.NotNil(t, rc.JudgeGroupID) {
		assert.Equal(t, g.ID, *rc.JudgeGroupID)
	}

	// Queue contains the legacy judge model (is_judge=true winner, id=1).
	var item ModelGroupItem
	assert.NoError(t, db.Where("group_id = ?", g.ID).First(&item).Error)
	assert.Equal(t, uint(1), item.ModelID)

	// Legacy columns dropped.
	assert.False(t, db.Migrator().HasColumn(&Model{}, "is_judge"))
	assert.False(t, db.Migrator().HasColumn(&RoutingConfig{}, "judge_model_id"))
}

func TestMigrateLegacyJudgeIdempotent(t *testing.T) {
	db := openLegacyDB(t)
	db.Exec("INSERT INTO providers (id, name, enabled, created_at) VALUES (1, 'p', 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (1, 'judge-mini', 1, 1, 1, datetime())")
	db.Exec("INSERT INTO routing_configs (id, judge_model_id, judge_group_id) VALUES (1, 1, NULL)")

	assert.NoError(t, migrateLegacyJudge(db))
	// Second run: both legacy columns gone -> early return, no error, no side effects.
	assert.NoError(t, migrateLegacyJudge(db))

	var n int64
	db.Model(&ModelGroup{}).Where("name = ?", "judge").Count(&n)
	assert.Equal(t, int64(1), n) // still exactly one judge queue
}

func TestMigrateLegacyJudgeFallsBackToJudgeModelID(t *testing.T) {
	// is_judge column exists but no row has is_judge=true; fall back to judge_model_id.
	db := openLegacyDB(t)
	db.Exec("INSERT INTO providers (id, name, enabled, created_at) VALUES (1, 'p', 1, datetime())")
	db.Exec("INSERT INTO models (id, name, provider_id, enabled, is_judge, created_at) VALUES (2, 'gpt-4o', 1, 1, 0, datetime())")
	db.Exec("INSERT INTO routing_configs (id, judge_model_id, judge_group_id) VALUES (1, 2, NULL)")

	err := migrateLegacyJudge(db)
	assert.NoError(t, err)

	var g ModelGroup
	assert.NoError(t, db.Where("name = ?", "judge").First(&g).Error)
	var item ModelGroupItem
	assert.NoError(t, db.Where("group_id = ?", g.ID).First(&item).Error)
	assert.Equal(t, uint(2), item.ModelID) // fell back to judge_model_id=2
}

func TestMigrateLegacyJudgeNoOpWhenNoLegacy(t *testing.T) {
	// Neither legacy column present -> no-op.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&Model{}, &RoutingConfig{}, &ModelGroup{}, &ModelGroupItem{}))
	db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1})

	assert.NoError(t, migrateLegacyJudge(db))
	assert.False(t, db.Migrator().HasColumn(&Model{}, "is_judge"))
	assert.False(t, db.Migrator().HasColumn(&RoutingConfig{}, "judge_model_id"))
}
```

- [ ] **Step 3: 构建与测试验证**

Run:

```
go build ./...
go vet ./...
go test ./internal/store/...
```

Expected: 4 个新迁移测试 + 既有 store 测试全部 PASS。

- [ ] **Step 4: Commit**

```
git add internal/store/store.go internal/store/migrate_judge_test.go
git commit -m "feat: auto-migrate legacy judge model to judge queue" -m "migrateLegacyJudge runs in store.Open after AutoMigrate. Prefers models.is_judge (the legacy engine's true source) over routing_configs.judge_model_id, creates a 'judge' queue containing the legacy judge model, points judge_group_id at it, then DropColumn both legacy columns. Idempotent: a no-op when neither column exists."
```

---

## Task 3: 前端判定选择器改为队列

**Files:**
- Modify: `web/src/api/routing.ts`
- Modify: `web/src/api/models.ts`
- Modify: `web/src/pages/Routing.tsx`
- Modify: `web/src/pages/Sources.tsx`

- [ ] **Step 1: 改 `web/src/api/routing.ts` — 字段重命名**

将 `RoutingConfig` 接口中 `judge_model_id: number | null` 改为 `judge_group_id: number | null`。替换后的接口：

```ts
export interface RoutingConfig {
  id: number
  judge_group_id: number | null
  default_group_id: number | null
  judge_max_input_chars: number
  gateway_token: string
}
```

- [ ] **Step 2: 改 `web/src/api/models.ts` — 删 is_judge 字段与 setJudgeModel 函数**

删除 `Model` 接口中 `is_judge: boolean` 行，删除整个 `setJudgeModel` 函数（第 31-33 行）。替换后的 `Model` 接口与函数段：

```ts
export interface Model {
  id: number
  name: string
  provider_id: number
  description: string
  enabled: boolean
}

export async function listModels(): Promise<Model[]> {
  const { data } = await apiClient.get('/admin/models')
  return data.data
}

export async function createModel(m: Partial<Model>): Promise<Model> {
  const { data } = await apiClient.post('/admin/models', m)
  return data
}

export async function updateModel(id: number, m: Partial<Model>): Promise<Model> {
  const { data } = await apiClient.put(`/admin/models/${id}`, m)
  return data
}

export async function deleteModel(id: number): Promise<void> {
  await apiClient.delete(`/admin/models/${id}`)
}
```

（`ModelTestUsage`/`ModelTestResult`/`testModel` 保持不变。）

- [ ] **Step 3: 改 `web/src/pages/Routing.tsx` — 判定选择器改为队列下拉**

3a. 删除 `listModels` import 与 `models` query（不再需要）。修改 import 行：

```tsx
import { getRoutingConfig, updateRoutingConfig } from '../api/routing'
import { listGroups } from '../api/groups'
```

删除以下 `useQuery` 块：

```tsx
  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: listModels,
  })
```

3b. 删除 `enabledModels` 与 `modelOptions` 派生（约第 54-55 行）：

```tsx
  const enabledModels = (models ?? []).filter((m) => m.enabled)
  const modelOptions = enabledModels.map((m) => ({ value: m.id, label: m.name }))
```

3c. 把副标题中的"判定模型"改为"判定队列"（约第 71 行）：

```tsx
      <div className="page-subtitle">配置智能路由的判定队列、兜底策略与 API Key</div>
```

3d. 替换"判定模型" `Form.Item` 为"判定队列"，使用 `groupOptions`：

```tsx
          <Form.Item
            name="judge_group_id"
            label={
              <Space size={4}>
                <span>判定队列</span>
                <Tooltip title="判定队列，按队列内模型顺序失败转移；建议选非推理模型组成的队列，推理模型会消耗大量 token 用于思考">
                  <InfoCircleOutlined style={{ color: 'var(--amber)', fontSize: 13 }} />
                </Tooltip>
              </Space>
            }
            extra={<Text type="secondary" style={{ fontSize: 12 }}>判定按队列内模型顺序失败转移，全部失败再回退兜底队列</Text>}
          >
            <Select allowClear placeholder="选择判定队列" options={groupOptions} />
          </Form.Item>
```

`default_group_id` 的 `Form.Item` 与其余表单保持不变。

- [ ] **Step 4: 改 `web/src/pages/Sources.tsx` — 删判定列与 judgeModelId 派生**

4a. 删除 `getRoutingConfig` import（第 20 行）：

```tsx
import { getRoutingConfig } from '../api/routing'
```

4b. 删除 `routingConfig` query 与 `judgeModelId` 派生（第 39-44 行）：

```tsx
  // 判定模型统一从 routingConfig 派生（单一数据源，避免与路由配置页冲突）
  const { data: routingConfig } = useQuery({
    queryKey: ['routingConfig'],
    queryFn: getRoutingConfig,
  })
  const judgeModelId = routingConfig?.judge_model_id ?? null
```

4c. 在 `modelColumns` 中删除"判定"列（第 195-198 行）：

```tsx
    {
      title: '判定', key: 'is_judge',
      render: (_: unknown, r: ModelType) => judgeModelId === r.id ? <Tag color="blue">判定模型</Tag> : null,
    },
```

4d. 把 `Table` 的 `rowClassName` 与 `onRow` 中 `judgeModelId` 逻辑去掉，改为普通属性：

```tsx
            rowClassName={() => ''}
            onRow={() => ({})}
```

（若 `Tag` 在文件其余地方不再使用，保留 import 无害；不强行删除以最小化改动。）

- [ ] **Step 5: 前端构建验证**

Run（在 `web/` 目录）:

```
npm run build
```

Expected: 构建成功，无 TypeScript 错误（重点检查 `judge_model_id` 已全部改为 `judge_group_id`、`is_judge`/`setJudgeModel` 已无引用、`listModels` 在 Routing.tsx 已无引用）。

若 IDE 报 `Tag` 未使用等 lint 警告，按提示清理 Sources.tsx 中不再使用的 `Tag` import（仅当确无其他使用时）。

- [ ] **Step 6: Commit**

```
git add web/src/api/routing.ts web/src/api/models.ts web/src/pages/Routing.tsx web/src/pages/Sources.tsx
git commit -m "refactor(web): switch judge selector to queue" -m "Routing.tsx: judge_model_id -> judge_group_id, selector now lists enabled queues, copy updated to reflect in-queue failover. Sources.tsx: drop the judge column and judgeModelId derivation. api/models.ts: drop is_judge field and setJudgeModel()."
```

---

## Task 4: 文档与记忆同步（强制）

**Files:**
- Modify: `README.md`
- Modify: `MEMORY.md`（若存在）
- Modify: `c:\Users\injoy\.trae-cn\memory\projects\-d-GOPATH-src-github-com-injoyai-auto-router\project_memory.md`

- [ ] **Step 1: 改 `README.md` — 步骤 3 与功能特性文案**

1a. 第 3 行"功能特性"前置描述中"由'判定模型'选择"改为"由'判定队列'选择"：

```markdown
AI 模型路由网关。请求到达时,由"判定队列"选择最合适的模型队列执行,支持 Agent 显式指定。对外兼容 OpenAI 协议。
```

1b. "使用"步骤 3 整行替换（移除 `POST /admin/models/:id/judge`，改为建判定队列 + 路由配置选判定队列）：

```markdown
3. 创建一个判定队列(含用于判定的模型,建议为非推理模型),在路由配置中选择该判定队列,并设置默认兜底队列(`PUT /admin/routing`)。
```

1c. "功能特性"中"由判定模型根据用户任务自动选择"改为"由判定队列根据用户任务自动选择":

```markdown
- **智能路由**:由判定队列根据用户任务自动选择最合适的队列
```

"路由模式"表格无需改动（override/judge/fallback 语义不变）。

- [ ] **Step 2: 同步 `MEMORY.md`（项目根）**

先读取现有 `MEMORY.md`，在判定相关条目处更新（若旧条目提到"判定模型"/`JudgeModelID`/`is_judge`/`SetJudgeModel`，替换为判定队列化后的现状）。若不存在该文件则按 AGENTS.md 5.5 创建。需记录的事实性信息：

- 路由判定由"单模型"改为"判定队列"：`RoutingConfig.JudgeGroupID` 指向 `ModelGroup`
- `JudgeClient.Judge(chain, ...)` 返回 `(raw, servedModel, usage, err)`，失败转移封装在 `lazyJudge` 内（方案 A）
- 已删除：`Model.IsJudge`、`SetJudgeModel`/`GetJudgeModel`/`IsModelReferenced`、`POST /admin/models/:id/judge`
- `migrateLegacyJudge` 在 `store.Open` 中执行，以 `is_judge` 为首选源迁移，再 DropColumn 两列
- `defaultJudgeClient` 降级为内部辅助（不再实现 `JudgeClient` 接口），单模型签名不变
- 构建候选时排除判定队列自身（`g.ID == *rc.JudgeGroupID`）

- [ ] **Step 3: 同步跨会话 `project_memory.md`**

路径：`c:\Users\injoy\.trae-cn\memory\projects\-d-GOPATH-src-github-com-injoyai-auto-router\project_memory.md`

先读取现状，追加/更新判定队列化相关条目（与 Step 2 同样的关键事实），保持简洁，避免堆砌流水账。若该文件已记录"判定模型"旧约定，替换为新约定。

- [ ] **Step 4: Commit**

```
git add README.md MEMORY.md
git commit -m "docs: update README and memory for judge queue" -m "README: step 3 now creates a judge queue and selects it in routing config; feature copy reflects judge queue. MEMORY.md synced with the judge-queue refactor per AGENTS.md 5.5."
```

（`project_memory.md` 位于 `.trae-cn` 目录、不在仓库内，无需 git 提交，Step 3 编辑保存即完成。）

---

## Self-Review 结果

**1. Spec 覆盖核对：**
- §3 数据模型与迁移 → Task 1 Step 1-2（字段/方法）+ Task 2（migrateLegacyJudge）
- §4 路由引擎改动（StoreDeps/JudgeClient/Route/defaultJudgeClient/lazyJudge）→ Task 1 Step 3-6
- §5 Admin API 与端点清理 → Task 1 Step 6-7
- §6 前端 → Task 3
- §7 日志（gateway.go 不改动）→ 无需任务，已确认
- §7 文档/记忆 → Task 4
- §7 测试 → Task 1 Step 5/8/9 + Task 2 Step 2
- §8 文件总览 → File Structure 表逐一对应
- §9 不在范围内 → 计划未引入隔离校验、未做判定队列为空阻塞、未改 Dispatcher/gateway 循环、未加前端测试 ✓

**2. 占位符扫描：** 无 TBD/TODO/"类似 Task N"/"添加适当错误处理"等；每个 step 含完整代码或确切命令。

**3. 类型一致性：**
- `JudgeClient.Judge` 签名 `(chain []*store.Model, candidates []Candidate, userText string) (raw string, servedModel string, usage *model.Usage, err error)` 在 engine.go（Task1 Step3）、lazyJudge（Task1 Step6）、fakeJudge（Task1 Step5）三处一致 ✓
- `RoutingConfig.JudgeGroupID *uint` 在 store/routing.go、engine Route、admin handleUpdateRouting、前端 api/routing.ts 四处一致 ✓
- `migrateLegacyJudge` 在 store.go 定义与 Open 调用一致 ✓
- `defaultJudgeClient.Judge` 单模型签名在 judge_client.go 与 judge_client_test.go 一致（均未改）✓
