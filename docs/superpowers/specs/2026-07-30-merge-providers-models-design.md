# 合并 API 源 + 模型管理页面设计

**日期：** 2026-07-30
**状态：** 已确认，待实施
**关联：** 基于 Plan 3 前端（`docs/superpowers/specs/2026-07-29-auto-router-frontend-design.md`）的改进

## 1. 背景与动机

当前管理后台将"API 源"和"模型"拆分为两个独立菜单。用户添加模型时需先去 API 源页创建源，再切到模型页选源添加模型，上下文割裂、操作繁琐。本设计将两者合并为一个"模型管理"页面，采用左右分栏布局，并新增"测试模型"功能。

## 2. 页面结构与路由

### 菜单变化
- **删除** 侧边栏 "API 源"（`/providers`）和 "模型"（`/models`）两个菜单项
- **新增** "模型管理" 菜单项（`/sources`），合并两者功能
- 其余菜单（仪表盘 / 路由配置 / 日志）不变

### 路由
- 新路由 `/sources` → 新页面 `Sources.tsx`
- 删除旧路由 `/providers` 和 `/models`

### 文件变化
| 文件 | 操作 |
|------|------|
| `web/src/pages/Sources.tsx` | **新建** — 合并后的左右分栏页面 |
| `web/src/pages/Providers.tsx` | **删除** |
| `web/src/pages/Models.tsx` | **删除** |
| `web/src/components/Layout.tsx` | **修改** — 菜单项改为 4 个 |
| `web/src/App.tsx` | **修改** — 路由表更新 |
| `web/src/api/models.ts` | **修改** — 新增 `testModel` 函数 |
| `internal/server/admin.go` | **修改** — 新增 `handleTestModel` |
| `internal/server/server.go` | **修改** — 注册 `POST /admin/models/:id/test` |

## 3. 左右分栏布局

### 左侧：API 源列表（宽度 280px）

- 顶部：标题 "API 源" + "添加" 按钮（PlusOutlined）
- 列表项（垂直排列）：
  - 名称 + 协议 Tag（openai 蓝 / claude 紫）
  - 未启用项灰色显示
  - 选中项高亮（青色背景 #e6fffb）
  - 悬停显示操作：[测试连通] [编辑] [删除]
- 底部：添加 API 源按钮

### 右侧：模型列表（剩余宽度）

- 顶部：标题 "OpenAI 的模型"（显示当前选中源名称）+ "添加模型" 按钮
- Table 列：
  - 名称（model id）
  - 显示名
  - 描述（ellipsis）
  - 判定（Tag "判定模型" if is_judge）
  - 启用（Switch）
  - 操作：[测试] [设为判定] [编辑] [删除]
- 判定模型行高亮 #e6fffb
- 未选中任何 API 源时：空状态提示"请先选择左侧的 API 源"

### 弹窗

- **API 源弹窗**：名称、Base URL、API Key（密码框，编辑时非必填）、协议（Select）、启用开关
- **模型弹窗**：名称、显示名、API 源（Select，**自动锁定为当前选中源**）、描述、启用
- **测试连通弹窗**：Result 成功/失败 + HTTP 状态码
- **测试模型弹窗**：Result 成功/失败 + 耗时 + 错误信息

### 关键交互

- 切换左侧选中源 → 右侧表格立即刷新
- 删除 API 源：若有关联模型则 409 提示
- 删除模型：若被路由配置引用则 409 提示
- 启用开关切换：发送完整记录 `{...r, enabled: v}`（保留 Plan 3 的 critical 修复）

## 4. 测试模型功能

### 后端：`POST /admin/models/:id/test`

**流程：**
1. 从 store 查询 model，拿到 `provider_id`
2. 查询 provider，解密 API key
3. 根据 provider 协议构建最小请求体：
   - OpenAI: `{"model": "<model.name>", "messages": [{"role":"user","content":"hi"}], "max_tokens": 1}`
   - Claude: `{"model": "<model.name>", "messages": [{"role":"user","content":"hi"}], "max_tokens": 1}`
4. 通过 `Dispatcher.Call` 发送请求（非流式），5 秒超时
5. 返回结果

**响应格式：**
```json
// 成功
{ "ok": true, "status": 200, "latency_ms": 823 }

// 失败
{ "ok": false, "status": 401, "error": "Invalid API key" }
```

**错误处理：**
- 上游 4xx/5xx：`ok: false`，`error` 为脱敏后的错误消息
- 网络错误/超时：`ok: false`，`status: 0`，`error: "timeout" 或 "network error"`
- 模型/provider 不存在：HTTP 404

**复用：** `Dispatcher.Call`、`upstreamPath`、`setUpstreamAuthHeaders`、`store.Decrypt`

### 前端：API 模块

`web/src/api/models.ts` 新增：
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

### 前端：测试模型弹窗

- 成功：`<Result status="success" title="模型可用" subTitle="耗时 823ms" />`
- 失败：`<Result status="error" title="模型不可用" subTitle={error} />`
- Loading：显示"正在测试..."

## 5. 不在范围内

- 不修改 Dashboard / Routing / Logs 页面
- 不修改路由引擎、协议适配等后端核心
- 不增加前端测试（与 Plan 3 一致，YAGNI）
