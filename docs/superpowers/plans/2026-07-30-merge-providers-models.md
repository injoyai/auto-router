# 合并 API 源 + 模型管理页面 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将"API 源"和"模型"两个独立菜单合并为单一"模型管理"页面（左右分栏布局），并新增"测试模型"功能。

**Architecture:** 前端新增 `Sources.tsx` 页面（左侧 API 源列表 + 右侧模型表格），复用 Plan 3 的 API 模块和 mutation 模式。后端新增 `POST /admin/models/:id/test` 端点，通过 Dispatcher 发送最小请求验证模型可用性。删除旧的 `Providers.tsx` 和 `Models.tsx`。

**Tech Stack:** Go (Gin) + React + TypeScript + Ant Design 5 + TanStack Query

---

## File Structure

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/upstream/dispatcher.go` | 修改 | 新增 `TestModel` 方法 |
| `internal/server/admin.go` | 修改 | 新增 `handleTestModel` |
| `internal/server/server.go` | 修改 | 注册 `POST /admin/models/:id/test` |
| `internal/upstream/dispatcher_test.go` | 修改 | 新增 `TestTestModel` 测试 |
| `web/src/api/models.ts` | 修改 | 新增 `testModel` 函数 + `ModelTestResult` 接口 |
| `web/src/pages/Sources.tsx` | 新建 | 合并后的左右分栏页面 |
| `web/src/pages/Providers.tsx` | 删除 | 被合并 |
| `web/src/pages/Models.tsx` | 删除 | 被合并 |
| `web/src/components/Layout.tsx` | 修改 | 菜单 5→4 项 |
| `web/src/App.tsx` | 修改 | 路由更新 |

---

## Task 1: 后端 — Dispatcher.TestModel + 测试

**Files:**
- Modify: `internal/upstream/dispatcher.go` (在 `TestConnect` 函数后新增 `TestModel`)
- Test: `internal/upstream/dispatcher_test.go` (新增 `TestTestModel`)

- [ ] **Step 1: 写失败测试**

在 `internal/upstream/dispatcher_test.go` 末尾追加：

```go
func TestTestModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "gpt-4o", body["model"])
		assert.Equal(t, float64(1), body["max_tokens"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"h"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	d := New()
	status, err := d.TestModel(srv.URL, "sk-test", "openai", "gpt-4o")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
}

func TestTestModelError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Invalid API key"}}`)
	}))
	defer srv.Close()

	d := New()
	status, err := d.TestModel(srv.URL, "sk-bad", "openai", "gpt-4o")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, status)
}
```

同时在 test 文件顶部 import 块加入 `"encoding/json"`（如尚未存在）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/upstream/ -run TestTestModel -v`
Expected: FAIL — `d.TestModel undefined`

- [ ] **Step 3: 实现 TestModel**

在 `internal/upstream/dispatcher.go` 的 `TestConnect` 函数后（第 134 行后）新增：

```go
// TestModel sends a minimal chat request to verify the model is usable.
// Returns the HTTP status code; caller interprets 2xx as success.
func (d *Dispatcher) TestModel(baseURL, apiKey, protocol, modelName string) (int, error) {
	body := map[string]any{
		"model":      modelName,
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"max_tokens": 1,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	path := upstreamPath(protocol)
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/upstream/ -run TestTestModel -v`
Expected: PASS (2 tests)

- [ ] **Step 5: 提交**

```bash
git add internal/upstream/dispatcher.go internal/upstream/dispatcher_test.go
git commit -m "feat(upstream): TestModel — minimal chat request to verify model availability"
```

---

## Task 2: 后端 — handleTestModel + 路由注册

**Files:**
- Modify: `internal/server/admin.go` (在 `handleTestProvider` 后新增 `handleTestModel`)
- Modify: `internal/server/server.go` (注册路由)

- [ ] **Step 1: 实现 handleTestModel**

在 `internal/server/admin.go` 的 `handleTestProvider` 函数后新增：

```go
func (a *App) handleTestModel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	m, err := a.Store.GetModel(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	prov, err := a.Store.GetProvider(m.ProviderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	apiKey, _ := store.Decrypt(a.CryptoKey, prov.APIKey)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()
	status, err := a.Dispatcher.TestModelCtx(ctx, prov.BaseURL, apiKey, prov.Protocol, m.Name)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":         false,
			"status":     0,
			"latency_ms": latency,
			"error":      sanitizeErr(err.Error()),
		})
		return
	}

	ok := status >= 200 && status < 300
	resp := gin.H{
		"ok":         ok,
		"status":     status,
		"latency_ms": latency,
	}
	if !ok {
		resp["error"] = fmt.Sprintf("HTTP %d", status)
	}
	c.JSON(http.StatusOK, resp)
}
```

Note: 需要 `context` 包。检查 `admin.go` 顶部 import 块是否已导入 `"context"`、`"time"`、`"auto-router/internal/upstream"`。若未导入则补充。

还需在 `dispatcher.go` 中新增 `TestModelCtx`（context 感知版本），在 `TestModel` 函数后。这个版本用 `http.NewRequestWithContext` 支持 5 秒超时取消：

```go
// TestModelCtx is the context-aware variant of TestModel.
func (d *Dispatcher) TestModelCtx(ctx context.Context, baseURL, apiKey, protocol, modelName string) (int, error) {
	body := map[string]any{
		"model":      modelName,
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"max_tokens": 1,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	path := upstreamPath(protocol)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	setUpstreamAuthHeaders(req, apiKey, protocol)
	resp, err := d.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
```

同时需要在 `admin.go` 中新增 `sanitizeErr` 辅助函数（若尚不存在）：

```go
// sanitizeErr strips potentially sensitive details from upstream error messages.
func sanitizeErr(s string) string {
	// Keep it simple: just return the message as-is for now.
	// The Dispatcher already returns generic "upstream returned N" messages.
	return s
}
```

- [ ] **Step 2: 注册路由**

在 `internal/server/server.go` 的 `NewApp` 函数中，找到 `admin.GET("/models/:id/judge", ...)` 一行（或类似的模型路由注册处），在其附近新增：

```go
admin.POST("/models/:id/test", app.handleTestModel)
```

- [ ] **Step 3: 验证编译**

Run: `go build ./...`
Expected: exit 0

- [ ] **Step 4: 运行测试确认无回归**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/server/admin.go internal/server/server.go internal/upstream/dispatcher.go
git commit -m "feat(server): POST /admin/models/:id/test endpoint for model testing"
```

---

## Task 3: 前端 API — 新增 testModel

**Files:**
- Modify: `web/src/api/models.ts` (末尾追加)

- [ ] **Step 1: 追加 testModel 函数**

在 `web/src/api/models.ts` 末尾追加：

```ts
export interface ModelTestResult {
  ok: boolean
  status: number
  latency_ms: number
  error?: string
}

export async function testModel(id: number): Promise<ModelTestResult> {
  const { data } = await apiClient.post(`/admin/models/${id}/test`)
  return data
}
```

- [ ] **Step 2: 验证编译**

Run: `cd web && npx tsc --noEmit`
Expected: exit 0

- [ ] **Step 3: 提交**

```bash
git add web/src/api/models.ts
git commit -m "feat(web): add testModel API function"
```

---

## Task 4: 前端 — 创建 Sources.tsx 页面

**Files:**
- Create: `web/src/pages/Sources.tsx`

- [ ] **Step 1: 创建 Sources.tsx**

创建 `web/src/pages/Sources.tsx`，内容如下：

```tsx
import { useState } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Tag, Space, Popconfirm, message, Result, Empty, Spin } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { Provider as ProviderType } from '../api/providers'
import type { Model as ModelType, ModelTestResult } from '../api/models'
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  testProvider,
} from '../api/providers'
import {
  listModels,
  createModel,
  updateModel,
  deleteModel,
  setJudgeModel,
  testModel,
} from '../api/models'

const protocolColors: Record<string, string> = { openai: 'blue', claude: 'purple' }

export default function Sources() {
  const qc = useQueryClient()
  const { data: providers, isLoading: provLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: listProviders,
  })

  const [selectedProviderId, setSelectedProviderId] = useState<number | null>(null)

  const { data: models, isLoading: modelsLoading } = useQuery({
    queryKey: ['models', selectedProviderId],
    queryFn: listModels,
    enabled: selectedProviderId !== null,
  })

  // Provider mutations
  const createProvMut = useMutation({
    mutationFn: createProvider,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('创建成功') },
    onError: () => { message.error('创建失败') },
  })
  const updateProvMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ProviderType> }) => updateProvider(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('更新成功') },
    onError: () => { message.error('更新失败') },
  })
  const deleteProvMut = useMutation({
    mutationFn: deleteProvider,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('已删除') },
    onError: (err: any) => {
      if (err?.response?.status === 409) message.warning('该 API 源下存在模型，无法删除')
      else message.error('删除失败')
    },
  })

  // Model mutations
  const createModelMut = useMutation({
    mutationFn: createModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('创建成功') },
    onError: () => { message.error('创建失败') },
  })
  const updateModelMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ModelType> }) => updateModel(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('更新成功') },
    onError: () => { message.error('更新失败') },
  })
  const deleteModelMut = useMutation({
    mutationFn: deleteModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('已删除') },
    onError: (err: any) => {
      if (err?.response?.status === 409) message.warning('该模型正被路由配置引用，无法删除')
      else message.error('删除失败')
    },
  })
  const judgeMut = useMutation({
    mutationFn: setJudgeModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('已设置判定模型') },
    onError: () => { message.error('设置失败') },
  })

  // Provider modal state
  const [provModalOpen, setProvModalOpen] = useState(false)
  const [editingProv, setEditingProv] = useState<ProviderType | null>(null)
  const [provForm] = Form.useForm()

  // Model modal state
  const [modelModalOpen, setModelModalOpen] = useState(false)
  const [editingModel, setEditingModel] = useState<ModelType | null>(null)
  const [modelForm] = Form.useForm()

  // Provider test modal state
  const [provTestOpen, setProvTestOpen] = useState(false)
  const [provTestResult, setProvTestResult] = useState<{ ok: boolean; status: number; error?: string } | null>(null)
  const [provTestLoading, setProvTestLoading] = useState(false)

  // Model test modal state
  const [modelTestOpen, setModelTestOpen] = useState(false)
  const [modelTestResult, setModelTestResult] = useState<ModelTestResult | null>(null)
  const [modelTestLoading, setModelTestLoading] = useState(false)

  // Auto-select first provider when list loads
  if (providers && providers.length > 0 && selectedProviderId === null) {
    setSelectedProviderId(providers[0].id)
  }

  const selectedProvider = providers?.find((p) => p.id === selectedProviderId)
  const filteredModels = (models ?? []).filter((m) => m.provider_id === selectedProviderId)

  // Provider modal handlers
  const openCreateProv = () => {
    setEditingProv(null)
    provForm.resetFields()
    provForm.setFieldsValue({ protocol: 'openai', enabled: true })
    setProvModalOpen(true)
  }
  const openEditProv = (p: ProviderType) => {
    setEditingProv(p)
    provForm.setFieldsValue(p)
    setProvModalOpen(true)
  }
  const handleProvSubmit = async () => {
    const vals = await provForm.validateFields()
    if (editingProv) {
      updateProvMut.mutate({ id: editingProv.id, data: vals })
    } else {
      createProvMut.mutate(vals)
    }
    setProvModalOpen(false)
  }

  // Model modal handlers
  const openCreateModel = () => {
    if (!selectedProviderId) {
      message.warning('请先选择 API 源')
      return
    }
    setEditingModel(null)
    modelForm.resetFields()
    modelForm.setFieldsValue({ provider_id: selectedProviderId, enabled: true })
    setModelModalOpen(true)
  }
  const openEditModel = (m: ModelType) => {
    setEditingModel(m)
    modelForm.setFieldsValue(m)
    setModelModalOpen(true)
  }
  const handleModelSubmit = async () => {
    const vals = await modelForm.validateFields()
    if (editingModel) {
      updateModelMut.mutate({ id: editingModel.id, data: vals })
    } else {
      createModelMut.mutate(vals)
    }
    setModelModalOpen(false)
  }

  // Provider test handler
  const handleProvTest = async (id: number) => {
    setProvTestLoading(true)
    setProvTestOpen(true)
    setProvTestResult(null)
    try {
      const r = await testProvider(id)
      setProvTestResult(r)
    } catch {
      setProvTestResult({ ok: false, status: 0, error: '网络错误' })
    } finally {
      setProvTestLoading(false)
    }
  }

  // Model test handler
  const handleModelTest = async (id: number) => {
    setModelTestLoading(true)
    setModelTestOpen(true)
    setModelTestResult(null)
    try {
      const r = await testModel(id)
      setModelTestResult(r)
    } catch {
      setModelTestResult({ ok: false, status: 0, latency_ms: 0, error: '网络错误' })
    } finally {
      setModelTestLoading(false)
    }
  }

  const modelColumns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '显示名', dataIndex: 'display_name', key: 'display_name' },
    { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
    {
      title: '判定', dataIndex: 'is_judge', key: 'is_judge',
      render: (v: boolean) => v ? <Tag color="cyan">判定模型</Tag> : null,
    },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled',
      render: (_: boolean, r: ModelType) => (
        <Switch checked={r.enabled} onChange={(v) => updateModelMut.mutate({ id: r.id, data: { ...r, enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ModelType) => (
        <Space>
          <Button size="small" onClick={() => handleModelTest(r.id)}>测试</Button>
          {!r.is_judge && (
            <Popconfirm title="设为判定模型？" onConfirm={() => judgeMut.mutate(r.id)}>
              <Button size="small">设为判定</Button>
            </Popconfirm>
          )}
          <Button size="small" onClick={() => openEditModel(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteModelMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div style={{ display: 'flex', gap: 16, minHeight: 'calc(100vh - 112px)' }}>
      {/* Left: Provider list */}
      <div style={{ width: 280, flexShrink: 0 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
          <strong>API 源</strong>
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreateProv}>添加</Button>
        </div>
        {provLoading ? (
          <Spin />
        ) : providers && providers.length > 0 ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            {providers.map((p) => (
              <div
                key={p.id}
                onClick={() => setSelectedProviderId(p.id)}
                style={{
                  padding: '8px 12px',
                  borderRadius: 6,
                  cursor: 'pointer',
                  background: p.id === selectedProviderId ? '#e6fffb' : '#fafafa',
                  border: p.id === selectedProviderId ? '1px solid #87e8de' : '1px solid #f0f0f0',
                  opacity: p.enabled ? 1 : 0.5,
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontWeight: p.id === selectedProviderId ? 600 : 400 }}>{p.name}</span>
                  <Tag color={protocolColors[p.protocol] ?? 'default'} style={{ marginRight: 0 }}>{p.protocol}</Tag>
                </div>
                <Space size="small" style={{ marginTop: 4 }} onClick={(e) => e.stopPropagation()}>
                  <Button size="small" type="link" style={{ padding: 0 }} onClick={() => handleProvTest(p.id)}>测试</Button>
                  <Button size="small" type="link" style={{ padding: 0 }} onClick={() => openEditProv(p)}>编辑</Button>
                  <Popconfirm title="确认删除？" onConfirm={() => deleteProvMut.mutate(p.id)}>
                    <Button size="small" type="link" danger style={{ padding: 0 }}>删除</Button>
                  </Popconfirm>
                </Space>
              </div>
            ))}
          </div>
        ) : (
          <Empty description="暂无 API 源" />
        )}
      </div>

      {/* Right: Model table */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
          <strong>{selectedProvider ? `${selectedProvider.name} 的模型` : '模型'}</strong>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModel} disabled={!selectedProviderId}>
            添加模型
          </Button>
        </div>
        <Table
          columns={modelColumns}
          dataSource={filteredModels}
          rowKey="id"
          loading={modelsLoading}
          rowClassName={(r) => r.is_judge ? 'ant-table-row-selected' : ''}
          onRow={(r) => ({
            style: r.is_judge ? { backgroundColor: '#e6fffb' } : undefined,
          })}
          locale={{ emptyText: selectedProviderId ? '暂无模型' : '请先选择左侧的 API 源' }}
        />
      </div>

      {/* Provider Modal */}
      <Modal
        title={editingProv ? '编辑 API 源' : '添加 API 源'}
        open={provModalOpen}
        onOk={handleProvSubmit}
        onCancel={() => setProvModalOpen(false)}
        confirmLoading={createProvMut.isPending || updateProvMut.isPending}
      >
        <Form form={provForm} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true, message: '请输入地址' }]}>
            <Input placeholder="https://api.openai.com/v1" />
          </Form.Item>
          <Form.Item name="api_key" label="API Key" rules={[{ required: !editingProv, message: '请输入密钥' }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="protocol" label="协议" rules={[{ required: true }]}>
            <Select options={[{ value: 'openai', label: 'OpenAI' }, { value: 'claude', label: 'Claude' }]} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      {/* Model Modal */}
      <Modal
        title={editingModel ? '编辑模型' : '添加模型'}
        open={modelModalOpen}
        onOk={handleModelSubmit}
        onCancel={() => setModelModalOpen(false)}
        confirmLoading={createModelMut.isPending || updateModelMut.isPending}
      >
        <Form form={modelForm} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="发给上游的真实 model id" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="provider_id" label="API 源" rules={[{ required: true }]}>
            <Select
              disabled
              options={(providers ?? []).filter((p) => p.enabled).map((p) => ({ value: p.id, label: p.name }))}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea placeholder="给判定模型看的描述" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      {/* Provider Test Modal */}
      <Modal
        title="连通性测试"
        open={provTestOpen}
        footer={<Button onClick={() => setProvTestOpen(false)}>关闭</Button>}
        onCancel={() => setProvTestOpen(false)}
      >
        {provTestLoading && <p>正在测试...</p>}
        {provTestResult && (
          <Result
            status={provTestResult.ok ? 'success' : 'error'}
            title={provTestResult.ok ? '连接成功' : '连接失败'}
            subTitle={provTestResult.ok ? `HTTP ${provTestResult.status}` : (provTestResult.error ?? `HTTP ${provTestResult.status}`)}
          />
        )}
      </Modal>

      {/* Model Test Modal */}
      <Modal
        title="模型测试"
        open={modelTestOpen}
        footer={<Button onClick={() => setModelTestOpen(false)}>关闭</Button>}
        onCancel={() => setModelTestOpen(false)}
      >
        {modelTestLoading && <p>正在测试...</p>}
        {modelTestResult && (
          <Result
            status={modelTestResult.ok ? 'success' : 'error'}
            title={modelTestResult.ok ? '模型可用' : '模型不可用'}
            subTitle={modelTestResult.ok ? `耗时 ${modelTestResult.latency_ms}ms` : (modelTestResult.error ?? `HTTP ${modelTestResult.status}`)}
          />
        )}
      </Modal>
    </div>
  )
}
```

- [ ] **Step 2: 验证编译**

Run: `cd web && npx tsc --noEmit`
Expected: exit 0

- [ ] **Step 3: 提交**

```bash
git add web/src/pages/Sources.tsx
git commit -m "feat(web): Sources page — master-detail layout merging providers + models"
```

---

## Task 5: 前端 — 更新 Layout + App + 删除旧页面

**Files:**
- Modify: `web/src/components/Layout.tsx`
- Modify: `web/src/App.tsx`
- Delete: `web/src/pages/Providers.tsx`
- Delete: `web/src/pages/Models.tsx`

- [ ] **Step 1: 更新 Layout.tsx 菜单项**

在 `web/src/components/Layout.tsx` 中，修改 `menuItems` 数组和导入。

将导入中的图标替换：
- 移除 `ApiOutlined`、`RobotOutlined` 导入
- 新增 `AppstoreOutlined` 导入

修改 menuItems 为 4 项：

```tsx
import {
  DashboardOutlined,
  AppstoreOutlined,
  SettingOutlined,
  FileTextOutlined,
  LogoutOutlined,
} from '@ant-design/icons'

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/sources', icon: <AppstoreOutlined />, label: '模型管理' },
  { key: '/routing', icon: <SettingOutlined />, label: '路由配置' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
]
```

- [ ] **Step 2: 更新 App.tsx 路由**

在 `web/src/App.tsx` 中：

1. 替换导入 — 移除 `Providers`、`Models` 导入，新增 `Sources`：

```tsx
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Sources from './pages/Sources'
import Routing from './pages/Routing'
import Logs from './pages/Logs'
```

2. 替换嵌套路由 — 将 `providers` 和 `models` 子路由替换为 `sources`：

```tsx
<Route index element={<Dashboard />} />
<Route path="sources" element={<Sources />} />
<Route path="routing" element={<Routing />} />
<Route path="logs" element={<Logs />} />
```

- [ ] **Step 3: 删除旧页面文件**

删除 `web/src/pages/Providers.tsx` 和 `web/src/pages/Models.tsx`。

- [ ] **Step 4: 验证编译**

Run: `cd web && npx tsc --noEmit`
Expected: exit 0（确保无残留引用）

- [ ] **Step 5: 提交**

```bash
git add web/src/components/Layout.tsx web/src/App.tsx
git rm web/src/pages/Providers.tsx web/src/pages/Models.tsx
git commit -m "feat(web): merge providers+models into /sources menu, remove old pages"
```

---

## Task 6: 端到端验证

**Files:** 无代码改动

- [ ] **Step 1: 前端构建**

Run: `cd web && npm run build`
Expected: tsc + vite build 成功，输出 `web/dist/`

- [ ] **Step 2: 后端构建**

Run: `cd .. && go build ./cmd/router`
Expected: exit 0

- [ ] **Step 3: 后端测试**

Run: `go test ./...`
Expected: 全部 PASS，无回归

- [ ] **Step 4: 确认 git status 干净**

Run: `git status`
Expected: `web/dist/` 内容被 gitignore，工作区干净

- [ ] **Step 5: 若有修复则提交**

如果上述步骤发现问题需修复，提交修复。否则无需提交。

---

## Self-Review Notes

**Spec coverage:**
- ✅ 合并 API 源 + 模型为单一菜单 → Task 4 (Sources.tsx) + Task 5 (Layout/App)
- ✅ 左右分栏布局 → Task 4
- ✅ 测试模型功能（后端 + 前端）→ Task 1-2 (backend) + Task 3 (frontend API) + Task 4 (UI)
- ✅ 删除旧页面 → Task 5
- ✅ 保留 Plan 3 critical 修复（完整记录更新）→ Task 4 的 Switch onChange 用 `{...r, enabled: v}`

**Type consistency:**
- `ModelTestResult` 接口在 Task 3 定义，Task 4 引用 — 字段名 `ok`/`status`/`latency_ms`/`error` 一致
- `ProviderType`、`ModelType` 别名与 Plan 3 一致
- 后端 `TestModel`/`TestModelCtx` 方法签名一致

**无占位符：** 所有代码步骤均含完整代码。
