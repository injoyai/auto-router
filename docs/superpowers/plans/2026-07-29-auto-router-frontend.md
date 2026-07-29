# React 前端管理后台（Plan 3）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 auto-router 增加基于 React + Vite + Ant Design 的管理后台前端，通过 Go embed 单文件部署。

**Architecture:** `web/` 目录下的标准 Vite + React + TypeScript 项目。TanStack Query 管理服务端状态，React Router v6 管理前端路由，Axios 通过 JWT 拦截器对接后端 `/admin/*` API。`vite build` 输出到 `web/dist`，Go 通过 `embed.FS` 嵌入并在 `NoRoute` 中提供 SPA fallback。

**Tech Stack:** React 18, TypeScript 5, Vite 5, Ant Design 5, @ant-design/charts, TanStack Query 5, React Router 6, Axios.

**Prerequisite:** Plan 1 + Plan 2 已完成，后端所有管理 API 就绪。

---

## 文件结构

```
web/
├── package.json
├── vite.config.ts
├── tsconfig.json
├── tsconfig.node.json
├── index.html
└── src/
    ├── main.tsx                    # 入口：QueryClientProvider + ConfigProvider + BrowserRouter
    ├── App.tsx                     # 路由定义
    ├── vite-env.d.ts
    ├── theme.ts                    # Ant Design 清爽青主题 token
    ├── api/
    │   ├── client.ts              # Axios 实例 + JWT 拦截器
    │   ├── auth.ts                # POST /admin/login
    │   ├── providers.ts           # CRUD + test
    │   ├── models.ts              # CRUD + setJudge
    │   ├── routing.ts             # GET/PUT /admin/routing
    │   └── logs.ts                # GET /admin/logs, /admin/stats
    ├── pages/
    │   ├── Login.tsx              # 登录页
    │   ├── Dashboard.tsx          # 仪表盘（统计卡片 + 饼图 + 柱状图）
    │   ├── Providers.tsx          # API 源管理（表格 + CRUD 弹窗 + 测试）
    │   ├── Models.tsx             # 模型管理（表格 + CRUD 弹窗 + 设判定）
    │   ├── Routing.tsx            # 路由配置（表单）
    │   └── Logs.tsx               # 请求日志（表格 + 过滤 + 展开）
    └── components/
        └── Layout.tsx             # 侧边栏 + 内容区壳

# Go 后端改动
cmd/router/main.go                 # MODIFY: embed web/dist + 注册静态文件
internal/server/server.go          # MODIFY: SPA fallback + 开发模式 CORS 中间件
.gitignore                         # MODIFY: 添加 node_modules
```

---

### Task 1: 脚手架 + 依赖安装

**Files:**
- Create: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/tsconfig.node.json`
- Create: `web/vite.config.ts`
- Create: `web/index.html`
- Create: `web/src/vite-env.d.ts`

- [ ] **Step 1: 创建 package.json**

`web/package.json`:
```json
{
  "name": "auto-router-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "@ant-design/charts": "^1.4.3",
    "@ant-design/icons": "^5.5.1",
    "@tanstack/react-query": "^5.62.0",
    "antd": "^5.22.0",
    "axios": "^1.7.9",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.28.0"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "typescript": "^5.7.2",
    "vite": "^5.4.11"
  }
}
```

- [ ] **Step 2: 创建 tsconfig.json**

`web/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["src"]
}
```

- [ ] **Step 3: 创建 tsconfig.node.json**

`web/tsconfig.node.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2023"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true
  },
  "include": ["vite.config.ts"]
}
```

- [ ] **Step 4: 创建 vite.config.ts（含 proxy）**

`web/vite.config.ts`:
```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/v1': 'http://localhost:8080',
      '/admin': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
})
```

- [ ] **Step 5: 创建 index.html**

`web/index.html`:
```html
<!DOCTYPE html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Auto Router</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 6: 创建 src/vite-env.d.ts**

`web/src/vite-env.d.ts`:
```ts
/// <reference types="vite/client" />
```

- [ ] **Step 7: 安装依赖**

```bash
cd web && npm install
```

- [ ] **Step 8: 提交**

```bash
git add web/
git commit -m "feat(web): scaffold Vite + React + TypeScript project with deps"
```

---

### Task 2: Ant Design 主题 + API 客户端 + API 模块

**Files:**
- Create: `web/src/theme.ts`
- Create: `web/src/api/client.ts`
- Create: `web/src/api/auth.ts`
- Create: `web/src/api/providers.ts`
- Create: `web/src/api/models.ts`
- Create: `web/src/api/routing.ts`
- Create: `web/src/api/logs.ts`

- [ ] **Step 1: 创建主题配置**

`web/src/theme.ts`:
```ts
import type { ThemeConfig } from 'antd'

const theme: ThemeConfig = {
  token: {
    colorPrimary: '#13c2c2',
    colorLink: '#08979c',
    colorBgContainer: '#ffffff',
    colorBgLayout: '#f5f5f5',
    borderRadius: 6,
  },
}

export default theme
```

- [ ] **Step 2: 创建 Axios 实例**

`web/src/api/client.ts`:
```ts
import axios from 'axios'

const apiClient = axios.create({ baseURL: '' })

apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('admin_jwt')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

apiClient.interceptors.response.use(
  (resp) => resp,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('admin_jwt')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

export default apiClient
```

- [ ] **Step 3: 创建 API 模块 auth.ts**

`web/src/api/auth.ts`:
```ts
import apiClient from './client'

export interface LoginResponse {
  token: string
  expires_in: number
}

export async function login(adminToken: string): Promise<LoginResponse> {
  const { data } = await apiClient.post('/admin/login', { token: adminToken })
  return data
}
```

- [ ] **Step 4: 创建 API 模块 providers.ts**

`web/src/api/providers.ts`:
```ts
import apiClient from './client'

export interface Provider {
  id: number
  name: string
  base_url: string
  api_key?: string
  protocol: string
  enabled: boolean
}

export interface TestResult {
  ok: boolean
  status: number
  error?: string
}

export async function listProviders(): Promise<Provider[]> {
  const { data } = await apiClient.get('/admin/providers')
  return data.data
}

export async function createProvider(p: Partial<Provider>): Promise<Provider> {
  const { data } = await apiClient.post('/admin/providers', p)
  return data
}

export async function updateProvider(id: number, p: Partial<Provider>): Promise<Provider> {
  const { data } = await apiClient.put(`/admin/providers/${id}`, p)
  return data
}

export async function deleteProvider(id: number): Promise<void> {
  await apiClient.delete(`/admin/providers/${id}`)
}

export async function testProvider(id: number): Promise<TestResult> {
  const { data } = await apiClient.post(`/admin/providers/${id}/test`)
  return data
}
```

- [ ] **Step 5: 创建 API 模块 models.ts**

`web/src/api/models.ts`:
```ts
import apiClient from './client'

export interface Model {
  id: number
  name: string
  display_name: string
  provider_id: number
  description: string
  enabled: boolean
  is_judge: boolean
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

export async function setJudgeModel(id: number): Promise<void> {
  await apiClient.post(`/admin/models/${id}/judge`)
}
```

- [ ] **Step 6: 创建 API 模块 routing.ts**

`web/src/api/routing.ts`:
```ts
import apiClient from './client'

export interface RoutingConfig {
  id: number
  judge_model_id: number | null
  default_model_id: number | null
  enable_next_model_directive: boolean
  session_ttl_seconds: number
  judge_max_input_chars: number
}

export async function getRoutingConfig(): Promise<RoutingConfig> {
  const { data } = await apiClient.get('/admin/routing')
  return data
}

export async function updateRoutingConfig(rc: Partial<RoutingConfig>): Promise<RoutingConfig> {
  const { data } = await apiClient.put('/admin/routing', rc)
  return data
}
```

- [ ] **Step 7: 创建 API 模块 logs.ts**

`web/src/api/logs.ts`:
```ts
import apiClient from './client'

export interface RequestLog {
  id: number
  session_id: string
  client_protocol: string
  requested_model: string
  routed_model: string
  route_reason: string
  judge_raw: string
  status: number
  latency_ms: number
  error: string
  created_at: string
}

export interface ListLogsParams {
  page?: number
  page_size?: number
  reason?: string
  model?: string
}

export interface ListLogsResponse {
  data: RequestLog[]
  total: number
  page: number
  page_size: number
}

export interface Stats {
  total: number
  by_reason: { Reason: string; Count: number }[]
}

export async function listLogs(params: ListLogsParams): Promise<ListLogsResponse> {
  const { data } = await apiClient.get('/admin/logs', { params })
  return data
}

export async function getStats(): Promise<Stats> {
  const { data } = await apiClient.get('/admin/stats')
  return data
}
```

- [ ] **Step 8: 验证 TypeScript 编译**

```bash
cd web && npx tsc --noEmit
```

- [ ] **Step 9: 提交**

```bash
git add web/src/
git commit -m "feat(web): Ant Design theme + API client + all API modules"
```

---

### Task 3: Layout 组件

**Files:**
- Create: `web/src/components/Layout.tsx`

- [ ] **Step 1: 创建 Layout 组件**

`web/src/components/Layout.tsx`:
```tsx
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout as AntLayout, Menu, Button } from 'antd'
import {
  DashboardOutlined,
  ApiOutlined,
  RobotOutlined,
  SettingOutlined,
  FileTextOutlined,
  LogoutOutlined,
} from '@ant-design/icons'

const { Sider, Content } = AntLayout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/providers', icon: <ApiOutlined />, label: 'API 源' },
  { key: '/models', icon: <RobotOutlined />, label: '模型' },
  { key: '/routing', icon: <SettingOutlined />, label: '路由配置' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
]

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = location.pathname === '/' ? '/' : '/' + location.pathname.split('/').filter(Boolean)[0]

  const handleLogout = () => {
    localStorage.removeItem('admin_jwt')
    navigate('/login')
  }

  return (
    <AntLayout style={{ minHeight: '100vh' }}>
      <Sider width={220} theme="light" style={{ borderRight: '1px solid #f0f0f0' }}>
        <div style={{ padding: '16px', fontWeight: 700, fontSize: 16, color: '#08979c' }}>
          Auto Router
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          style={{ borderRight: 0 }}
        />
        <div style={{ position: 'absolute', bottom: 0, width: '100%', padding: '12px 16px' }}>
          <Button icon={<LogoutOutlined />} block onClick={handleLogout}>
            退出登录
          </Button>
        </div>
      </Sider>
      <AntLayout>
        <Content style={{ padding: 24 }}>
          <Outlet />
        </Content>
      </AntLayout>
    </AntLayout>
  )
}
```

- [ ] **Step 2: 验证 TypeScript 编译**

```bash
cd web && npx tsc --noEmit
```

- [ ] **Step 3: 提交**

```bash
git add web/src/components/
git commit -m "feat(web): sidebar layout component"
```

---

### Task 4: Login 页面 + App.tsx + main.tsx

**Files:**
- Create: `web/src/pages/Login.tsx`
- Create: `web/src/App.tsx`
- Create: `web/src/main.tsx`

- [ ] **Step 1: 创建 Login 页面**

`web/src/pages/Login.tsx`:
```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Input, Button, Alert, Typography } from 'antd'
import { login } from '../api/auth'

const { Title } = Typography

export default function Login() {
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const navigate = useNavigate()

  const handleSubmit = async () => {
    if (!token.trim()) return
    setLoading(true)
    setError('')
    try {
      const res = await login(token.trim())
      localStorage.setItem('admin_jwt', res.token)
      navigate('/')
    } catch {
      setError('登录失败，请检查 Token')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#f5f5f5',
      }}
    >
      <Card style={{ width: 400 }}>
        <Title level={4} style={{ textAlign: 'center', marginBottom: 24 }}>
          Auto Router 管理后台
        </Title>
        {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} showIcon />}
        <Input.Password
          placeholder="请输入 Admin Token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          onPressEnter={handleSubmit}
          style={{ marginBottom: 16 }}
          size="large"
        />
        <Button type="primary" block size="large" loading={loading} onClick={handleSubmit}>
          登录
        </Button>
      </Card>
    </div>
  )
}
```

- [ ] **Step 2: 创建 App.tsx**

`web/src/App.tsx`:
```tsx
import { Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Providers from './pages/Providers'
import Models from './pages/Models'
import Routing from './pages/Routing'
import Logs from './pages/Logs'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const token = localStorage.getItem('admin_jwt')
  if (!token) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <Layout />
          </ProtectedRoute>
        }
      >
        <Route index element={<Dashboard />} />
        <Route path="providers" element={<Providers />} />
        <Route path="models" element={<Models />} />
        <Route path="routing" element={<Routing />} />
        <Route path="logs" element={<Logs />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
```

- [ ] **Step 3: 创建 main.tsx**

`web/src/main.tsx`:
```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ConfigProvider } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import theme from './theme'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
  },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ConfigProvider theme={theme} locale={zhCN}>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </ConfigProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
```

- [ ] **Step 4: 验证 TypeScript 编译（此时 Dashboard 等页面尚未实现，先创建占位页面）**

创建占位页面文件（空的默认导出），否则 `tsc` 会报错。创建以下占位文件：

`web/src/pages/Dashboard.tsx`:
```tsx
export default function Dashboard() {
  return null
}
```

`web/src/pages/Providers.tsx`:
```tsx
export default function Providers() {
  return null
}
```

`web/src/pages/Models.tsx`:
```tsx
export default function Models() {
  return null
}
```

`web/src/pages/Routing.tsx`:
```tsx
export default function Routing() {
  return null
}
```

`web/src/pages/Logs.tsx`:
```tsx
export default function Logs() {
  return null
}
```

- [ ] **Step 5: 验证编译**

```bash
cd web && npx tsc --noEmit
```
Expected: exit 0，无报错。

- [ ] **Step 6: 提交**

```bash
git add web/src/
git commit -m "feat(web): login page + App router + main entry + page stubs"
```

---

### Task 5: Dashboard 页面（统计卡片 + 图表）

**Files:**
- Modify: `web/src/pages/Dashboard.tsx` (replace stub)

- [ ] **Step 1: 替换 Dashboard 为完整实现**

`web/src/pages/Dashboard.tsx`:
```tsx
import { Card, Col, Row, Statistic, Spin } from 'antd'
import { Pie, Column } from '@ant-design/charts'
import { useQuery } from '@tanstack/react-query'
import { getStats, listLogs } from '../api/logs'
import { listModels } from '../api/models'

export default function Dashboard() {
  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: ['stats'],
    queryFn: getStats,
  })

  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: listModels,
  })

  const { data: logsData, isLoading: logsLoading } = useQuery({
    queryKey: ['logs', 'dashboard'],
    queryFn: () => listLogs({ page: 1, page_size: 1000 }),
  })

  if (statsLoading || logsLoading) {
    return <Spin size="large" style={{ display: 'block', marginTop: 100 }} />
  }

  const successCount = logsData?.data.filter((l) => l.status < 400).length ?? 0
  const totalCount = stats?.total ?? 0
  const successRate = totalCount > 0 ? ((successCount / totalCount) * 100).toFixed(1) : '0'

  const avgLatency = (logsData?.data.length ?? 0) > 0
    ? Math.round(logsData!.data.reduce((sum, l) => sum + l.latency_ms, 0) / logsData!.data.length)
    : 0

  const activeModelCount = models?.filter((m) => m.enabled).length ?? 0

  const pieData = (stats?.by_reason ?? []).map((r) => ({
    type: r.Reason,
    value: r.Count,
  }))

  const modelMap: Record<string, number> = {}
  ;(logsData?.data ?? []).forEach((l) => {
    if (l.routed_model) {
      modelMap[l.routed_model] = (modelMap[l.routed_model] ?? 0) + 1
    }
  })
  const columnData = Object.entries(modelMap)
    .map(([model, count]) => ({ model, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 10)

  const pieConfig = {
    data: pieData,
    angleField: 'value',
    colorField: 'type',
    radius: 0.8,
    label: { type: 'outer' as const },
    legend: { position: 'bottom' as const },
  }

  const columnConfig = {
    data: columnData,
    xField: 'model',
    yField: 'count',
    label: { position: 'top' as const },
    xAxis: { label: { autoRotate: true, autoHide: false } },
  }

  return (
    <div>
      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col span={6}>
          <Card>
            <Statistic title="总请求数" value={totalCount} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="成功率" value={successRate} suffix="%" />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="活跃模型数" value={activeModelCount} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="平均延迟" value={avgLatency} suffix="ms" />
          </Card>
        </Col>
      </Row>
      <Row gutter={16}>
        <Col span={12}>
          <Card title="路由原因分布">
            {pieData.length > 0 ? (
              <Pie {...pieConfig} />
            ) : (
              <p style={{ color: '#999', textAlign: 'center', padding: 40 }}>暂无数据</p>
            )}
          </Card>
        </Col>
        <Col span={12}>
          <Card title="模型使用占比">
            {columnData.length > 0 ? (
              <Column {...columnConfig} />
            ) : (
              <p style={{ color: '#999', textAlign: 'center', padding: 40 }}>暂无数据</p>
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}
```

- [ ] **Step 2: 验证编译**

```bash
cd web && npx tsc --noEmit
```
Expected: exit 0。

- [ ] **Step 3: 提交**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat(web): dashboard with stats cards + pie/column charts"
```

---

### Task 6: Providers 页面（API 源管理）

**Files:**
- Modify: `web/src/pages/Providers.tsx` (replace stub)

- [ ] **Step 1: 替换 Providers 为完整实现**

`web/src/pages/Providers.tsx`:
```tsx
import { useState } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Tag, Space, Popconfirm, message, Result } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { Provider as ProviderType } from '../api/providers'
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  testProvider,
} from '../api/providers'

const protocolColors: Record<string, string> = { openai: 'blue', claude: 'purple' }

export default function Providers() {
  const qc = useQueryClient()
  const { data: providers, isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: listProviders,
  })

  const createMut = useMutation({
    mutationFn: createProvider,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('创建成功') },
  })
  const updateMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ProviderType> }) => updateProvider(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('更新成功') },
  })
  const deleteMut = useMutation({
    mutationFn: deleteProvider,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('已删除') },
    onError: (err: any) => {
      if (err?.response?.status === 409) message.warning('该 API 源下存在模型，无法删除')
      else message.error('删除失败')
    },
  })

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ProviderType | null>(null)
  const [form] = Form.useForm()

  // Test connectivity state
  const [testOpen, setTestOpen] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; status: number; error?: string } | null>(null)
  const [testLoading, setTestLoading] = useState(false)

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ protocol: 'openai', enabled: true })
    setModalOpen(true)
  }

  const openEdit = (p: ProviderType) => {
    setEditing(p)
    form.setFieldsValue(p)
    setModalOpen(true)
  }

  const handleSubmit = async () => {
    const vals = await form.validateFields()
    if (editing) {
      updateMut.mutate({ id: editing.id, data: vals })
    } else {
      createMut.mutate(vals)
    }
    setModalOpen(false)
  }

  const handleTest = async (id: number) => {
    setTestLoading(true)
    setTestOpen(true)
    try {
      const r = await testProvider(id)
      setTestResult(r)
    } catch {
      setTestResult({ ok: false, status: 0, error: '网络错误' })
    } finally {
      setTestLoading(false)
    }
  }

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Base URL', dataIndex: 'base_url', key: 'base_url', ellipsis: true },
    {
      title: '协议', dataIndex: 'protocol', key: 'protocol',
      render: (p: string) => <Tag color={protocolColors[p] ?? 'default'}>{p}</Tag>,
    },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled',
      render: (_: boolean, r: ProviderType) => (
        <Switch checked={r.enabled} onChange={(v) => updateMut.mutate({ id: r.id, data: { enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ProviderType) => (
        <Space>
          <Button size="small" onClick={() => handleTest(r.id)}>测试</Button>
          <Button size="small" onClick={() => openEdit(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加 API 源</Button>
      </div>
      <Table columns={columns} dataSource={providers} rowKey="id" loading={isLoading} />

      <Modal
        title={editing ? '编辑 API 源' : '添加 API 源'}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
        confirmLoading={createMut.isPending || updateMut.isPending}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true, message: '请输入地址' }]}>
            <Input placeholder="https://api.openai.com/v1" />
          </Form.Item>
          <Form.Item name="api_key" label="API Key" rules={[{ required: !editing, message: '请输入密钥' }]}>
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

      <Modal
        title="连通性测试"
        open={testOpen}
        footer={<Button onClick={() => setTestOpen(false)}>关闭</Button>}
        onCancel={() => setTestOpen(false)}
        confirmLoading={testLoading}
      >
        {testLoading && <p>正在测试...</p>}
        {testResult && (
          <Result
            status={testResult.ok ? 'success' : 'error'}
            title={testResult.ok ? '连接成功' : '连接失败'}
            subTitle={testResult.ok ? `HTTP ${testResult.status}` : (testResult.error ?? `HTTP ${testResult.status}`)}
          />
        )}
      </Modal>
    </div>
  )
}
```
(Add the `import { message } from 'antd'` to the imports — it is included in the above code via destructuring from antd.)

- [ ] **Step 2: 验证编译**

```bash
cd web && npx tsc --noEmit
```
Expected: exit 0。

- [ ] **Step 3: 提交**

```bash
git add web/src/pages/Providers.tsx
git commit -m "feat(web): providers page — table + CRUD modal + connectivity test"
```

---

### Task 7: Models 页面（模型管理）

**Files:**
- Modify: `web/src/pages/Models.tsx` (replace stub)

- [ ] **Step 1: 替换 Models 为完整实现**

`web/src/pages/Models.tsx`:
```tsx
import { useState } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Tag, Space, Popconfirm, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { Model as ModelType } from '../api/models'
import type { Provider } from '../api/providers'
import { listModels, createModel, updateModel, deleteModel, setJudgeModel } from '../api/models'
import { listProviders } from '../api/providers'

export default function Models() {
  const qc = useQueryClient()
  const { data: models, isLoading } = useQuery({ queryKey: ['models'], queryFn: listModels })
  const { data: providers } = useQuery({ queryKey: ['providers'], queryFn: listProviders })

  const createMut = useMutation({
    mutationFn: createModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('创建成功') },
  })
  const updateMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ModelType> }) => updateModel(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('更新成功') },
  })
  const deleteMut = useMutation({
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
  })

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ModelType | null>(null)
  const [form] = Form.useForm()

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ enabled: true })
    setModalOpen(true)
  }

  const openEdit = (m: ModelType) => {
    setEditing(m)
    form.setFieldsValue(m)
    setModalOpen(true)
  }

  const handleSubmit = async () => {
    const vals = await form.validateFields()
    if (editing) {
      updateMut.mutate({ id: editing.id, data: vals })
    } else {
      createMut.mutate(vals)
    }
    setModalOpen(false)
  }

  const providerMap = new Map((providers ?? []).map((p: Provider) => [p.id, p.name]))

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '显示名', dataIndex: 'display_name', key: 'display_name' },
    {
      title: 'API 源', dataIndex: 'provider_id', key: 'provider_id',
      render: (id: number) => providerMap.get(id) ?? '-',
    },
    { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
    {
      title: '判定', dataIndex: 'is_judge', key: 'is_judge',
      render: (v: boolean) => v ? <Tag color="cyan">判定模型</Tag> : null,
    },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled',
      render: (_: boolean, r: ModelType) => (
        <Switch checked={r.enabled} onChange={(v) => updateMut.mutate({ id: r.id, data: { enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ModelType) => (
        <Space>
          {!r.is_judge && (
            <Popconfirm title="设为判定模型？" onConfirm={() => judgeMut.mutate(r.id)}>
              <Button size="small">设为判定</Button>
            </Popconfirm>
          )}
          <Button size="small" onClick={() => openEdit(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加模型</Button>
      </div>
      <Table
        columns={columns}
        dataSource={models}
        rowKey="id"
        loading={isLoading}
        rowClassName={(r) => r.is_judge ? 'ant-table-row-selected' : ''}
        onRow={(r) => ({
          style: r.is_judge ? { backgroundColor: '#e6fffb' } : undefined,
        })}
      />

      <Modal
        title={editing ? '编辑模型' : '添加模型'}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
        confirmLoading={createMut.isPending || updateMut.isPending}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="发给上游的真实 model id" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="provider_id" label="API 源" rules={[{ required: true }]}>
            <Select
              options={(providers ?? []).filter((p: Provider) => p.enabled).map((p: Provider) => ({
                value: p.id,
                label: p.name,
              }))}
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
    </div>
  )
}
```

- [ ] **Step 2: 验证编译**

```bash
cd web && npx tsc --noEmit
```
Expected: exit 0。

- [ ] **Step 3: 提交**

```bash
git add web/src/pages/Models.tsx
git commit -m "feat(web): models page — table + CRUD modal + set judge"
```

---

### Task 8: Routing 页面（路由配置）

**Files:**
- Modify: `web/src/pages/Routing.tsx` (replace stub)

- [ ] **Step 1: 替换 Routing 为完整实现**

`web/src/pages/Routing.tsx`:
```tsx
import { useEffect } from 'react'
import { Card, Form, Select, Switch, InputNumber, Button, message, Spin, Tooltip } from 'antd'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getRoutingConfig, updateRoutingConfig } from '../api/routing'
import { listModels } from '../api/models'

export default function Routing() {
  const qc = useQueryClient()
  const [form] = Form.useForm()

  const { data: cfg, isLoading } = useQuery({
    queryKey: ['routingConfig'],
    queryFn: getRoutingConfig,
  })

  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: listModels,
  })

  useEffect(() => {
    if (cfg) form.setFieldsValue(cfg)
  }, [cfg, form])

  const saveMut = useMutation({
    mutationFn: updateRoutingConfig,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['routingConfig'] })
      message.success('保存成功')
    },
  })

  const handleSave = async () => {
    const vals = await form.validateFields()
    saveMut.mutate(vals)
  }

  if (isLoading) return <Spin size="large" style={{ display: 'block', marginTop: 48 }} />

  const modelOptions = (models ?? []).map((m) => ({ value: m.id, label: `${m.display_name} (${m.name})` }))

  return (
    <Card title="路由配置">
      <Form form={form} layout="vertical" style={{ maxWidth: 500 }}>
        <Form.Item name="judge_model_id" label="判定模型">
          <Select allowClear placeholder="选择判定模型" options={modelOptions} />
        </Form.Item>
        <Form.Item name="default_model_id" label="默认兜底模型">
          <Select allowClear placeholder="选择兜底模型" options={modelOptions} />
        </Form.Item>
        <Form.Item name="enable_next_model_directive" label="允许模型回选" valuePropName="checked">
          <Tooltip title="开启后，模型可在回复中用 <<next_model: 模型名>> 指定下一轮模型">
            <Switch />
          </Tooltip>
        </Form.Item>
        <Form.Item name="session_ttl_seconds" label="会话 TTL（秒）">
          <InputNumber min={60} max={86400} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="judge_max_input_chars" label="判定输入截断（字符）">
          <InputNumber min={100} max={10000} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item>
          <Button type="primary" onClick={handleSave} loading={saveMut.isPending}>保存</Button>
        </Form.Item>
      </Form>
    </Card>
  )
}
```

- [ ] **Step 2: 验证编译**

```bash
cd web && npx tsc --noEmit
```
Expected: exit 0。

- [ ] **Step 3: 提交**

```bash
git add web/src/pages/Routing.tsx
git commit -m "feat(web): routing config page with form"
```

---

### Task 9: Logs 页面（请求日志）

**Files:**
- Modify: `web/src/pages/Logs.tsx` (replace stub)

- [ ] **Step 1: 替换 Logs 为完整实现**

`web/src/pages/Logs.tsx`:
```tsx
import { useState } from 'react'
import { Table, Tag, Select, Input, Button, Space, Card } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { listLogs } from '../api/logs'
import type { RequestLog } from '../api/logs'

const reasonColors: Record<string, string> = {
  override: 'blue',
  session: 'green',
  judge: 'cyan',
  fallback: 'orange',
}

export default function Logs() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [reason, setReason] = useState<string | undefined>()
  const [model, setModel] = useState<string | undefined>()

  const [filters, setFilters] = useState({ reason: undefined as string | undefined, model: undefined as string | undefined })

  const { data, isLoading } = useQuery({
    queryKey: ['logs', page, pageSize, filters.reason, filters.model],
    queryFn: () => listLogs({ page, page_size: pageSize, reason: filters.reason, model: filters.model }),
  })

  const handleSearch = () => {
    setFilters({ reason, model })
    setPage(1)
  }

  const handleReset = () => {
    setReason(undefined)
    setModel(undefined)
    setFilters({ reason: undefined, model: undefined })
    setPage(1)
  }

  const columns = [
    {
      title: '时间', dataIndex: 'created_at', key: 'created_at', width: 170,
      render: (v: string) => v ? new Date(v).toLocaleString('zh-CN') : '-',
    },
    {
      title: '会话ID', dataIndex: 'session_id', key: 'session_id', width: 140,
      render: (v: string) => v?.slice(0, 12) ?? '-',
    },
    { title: '请求模型', dataIndex: 'requested_model', key: 'requested_model', width: 120 },
    { title: '路由模型', dataIndex: 'routed_model', key: 'routed_model', width: 120 },
    {
      title: '路由原因', dataIndex: 'route_reason', key: 'route_reason', width: 100,
      render: (v: string) => <Tag color={reasonColors[v] ?? 'default'}>{v}</Tag>,
    },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 70,
      render: (v: number) => <span style={{ color: v < 400 ? '#52c41a' : '#ff4d4f' }}>{v}</span>,
    },
    {
      title: '延迟', dataIndex: 'latency_ms', key: 'latency_ms', width: 80,
      render: (v: number) => `${v}ms`,
    },
    {
      title: '错误', dataIndex: 'error', key: 'error', width: 200, ellipsis: true,
      render: (v: string) => v ? <span style={{ color: '#ff4d4f' }}>{v}</span> : '-',
    },
  ]

  return (
    <div>
      <Card size="small" style={{ marginBottom: 16 }}>
        <Space wrap>
          <Select
            placeholder="路由原因"
            allowClear
            style={{ width: 140 }}
            value={reason}
            onChange={setReason}
            options={[
              { value: 'override', label: 'override' },
              { value: 'session', label: 'session' },
              { value: 'judge', label: 'judge' },
              { value: 'fallback', label: 'fallback' },
            ]}
          />
          <Input.Search
            placeholder="模型名"
            allowClear
            style={{ width: 200 }}
            value={model}
            onChange={(e) => setModel(e.target.value)}
          />
          <Button type="primary" onClick={handleSearch}>查询</Button>
          <Button onClick={handleReset}>重置</Button>
        </Space>
      </Card>

      <Table
        columns={columns}
        dataSource={data?.data}
        rowKey="id"
        loading={isLoading}
        scroll={{ x: 1200 }}
        expandable={{
          expandedRowRender: (r: RequestLog) => (
            r.judge_raw ? (
              <pre style={{ whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: 13, background: '#fafafa', padding: 12, borderRadius: 6 }}>
                {r.judge_raw}
              </pre>
            ) : (
              <p style={{ color: '#999' }}>无判定原始数据</p>
            )
          ),
          rowExpandable: (r: RequestLog) => !!r.judge_raw,
        }}
        pagination={{
          current: page,
          pageSize,
          total: data?.total ?? 0,
          showSizeChanger: true,
          showTotal: (total) => `共 ${total} 条`,
          onChange: (p, ps) => { setPage(p); setPageSize(ps) },
        }}
      />
    </div>
  )
}
```

- [ ] **Step 2: 验证编译**

```bash
cd web && npx tsc --noEmit
```
Expected: exit 0。

- [ ] **Step 3: 提交**

```bash
git add web/src/pages/Logs.tsx
git commit -m "feat(web): logs page with filters + pagination + expand row"
```

---

### Task 10: Go 后端 — embed + SPA fallback + 开发 CORS

**Files:**
- Modify: `cmd/router/main.go`
- Modify: `internal/server/server.go`

- [ ] **Step 1: 修改 server.go 添加 SPA fallback + CORS**

`internal/server/server.go` — 在 `NewApp` 函数末尾、`return app` 之前，添加 `NoRoute` handler 和 CORS 中间件逻辑。同时添加所需 imports。

完整修改后的 `NewApp` 函数（只改中间部分，lazyJudge 等其余代码不变）：

```go
import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-router/internal/config"
	"auto-router/internal/jwt"
	"auto-router/internal/routing"
	"auto-router/internal/store"
	"auto-router/internal/upstream"
)

// ...

func NewApp(cfg Config, st *store.Store, cryptoKey []byte, gatewayToken, adminToken string) *App {
	jwtMgr := jwt.New(adminToken)
	disp := upstream.New()
	engine := routing.New(st, &lazyJudge{st: st, disp: disp, key: cryptoKey})

	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// Dev mode: permissive CORS so the Vite dev server can call the backend.
	if cfg.ListenAddr == ":8080" {
		r.Use(func(c *gin.Context) {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
		})
	}

	app := &App{
		Router:       r,
		Store:        st,
		Engine:       engine,
		Dispatcher:   disp,
		JWT:          jwtMgr,
		CryptoKey:    cryptoKey,
		GatewayToken: gatewayToken,
		AdminToken:   adminToken,
	}

	v1 := r.Group("/v1", GatewayAuth(gatewayToken))
	v1.POST("/chat/completions", app.handleChatCompletions)
	v1.POST("/messages", app.handleMessages)
	v1.GET("/models", app.handleListModels)

	admin := r.Group("/admin")
	admin.POST("/login", app.handleAdminLogin)
	authAdmin := admin.Group("", AdminAuth(jwtMgr))
	authAdmin.GET("/providers", app.handleListProviders)
	authAdmin.POST("/providers", app.handleCreateProvider)
	authAdmin.PUT("/providers/:id", app.handleUpdateProvider)
	authAdmin.DELETE("/providers/:id", app.handleDeleteProvider)
	authAdmin.POST("/providers/:id/test", app.handleTestProvider)
	authAdmin.GET("/models", app.handleListModelsAdmin)
	authAdmin.POST("/models", app.handleCreateModel)
	authAdmin.PUT("/models/:id", app.handleUpdateModel)
	authAdmin.DELETE("/models/:id", app.handleDeleteModel)
	authAdmin.POST("/models/:id/judge", app.handleSetJudge)
	authAdmin.GET("/routing", app.handleGetRouting)
	authAdmin.PUT("/routing", app.handleUpdateRouting)
	authAdmin.GET("/logs", app.handleListLogs)
	authAdmin.GET("/stats", app.handleStats)

	return app
}

// ServeSPA registers a NoRoute handler to serve the embedded React SPA.
// webFS is the embedded filesystem from //go:embed.
func (a *App) ServeSPA(webFS fs.FS) {
	a.Router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/admin") || path == "/health" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		// Try serving the exact file; fall back to index.html for SPA routing.
		f, err := webFS.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			c.FileFromFS("/", http.FS(webFS))
			return
		}
		f.Close()
		c.FileFromFS(path, http.FS(webFS))
	})
}
```

- [ ] **Step 2: 修改 main.go 添加 embed + ServeSPA 调用**

`cmd/router/main.go`:
```go
package main

import (
	"embed"
	"io/fs"
	"log"
	"time"

	"auto-router/internal/config"
	"auto-router/internal/server"
	"auto-router/internal/store"
)

//go:embed web/dist/*
var webFS embed.FS

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	key, gwToken, adminToken, err := server.Bootstrap(st)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.GatewayToken != "" {
		gwToken = cfg.GatewayToken
	}
	if cfg.AdminToken != "" {
		adminToken = cfg.AdminToken
	}
	app := server.NewApp(cfg, st, key, gwToken, adminToken)
	server.StartSessionCleanup(st, time.Minute)

	// Serve embedded React SPA (non-API requests fall back to index.html).
	if webSub, err := fs.Sub(webFS, "web/dist"); err == nil {
		app.ServeSPA(webSub)
		log.Println("SPA static files enabled (embedded)")
	} else {
		log.Printf("SPA static files not available: %v", err)
	}

	log.Printf("listening on %s | gateway token: %s | admin token: %s", cfg.ListenAddr, gwToken, adminToken)
	if err := app.Router.Run(cfg.ListenAddr); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 3: 更新 .gitignore**

在 `.gitignore` 末尾添加：
```
# Frontend
node_modules/
web/dist/
```

- [ ] **Step 4: 验证 Go 编译**

```bash
go build ./...
```
Expected: exit 0。embed 指令在没有 web/dist 目录时会编译失败 — 你需要先创建空目录：
```bash
mkdir -p web/dist
```
然后编译。

- [ ] **Step 5: 提交**

```bash
git add cmd/router/main.go internal/server/server.go .gitignore
git commit -m "feat(server): Go embed SPA + NoRoute fallback + dev CORS"
```

---

### Task 11: 端到端构建验证

**Files:** None (build verification only)

- [ ] **Step 1: 构建前端**

```bash
cd web && npm run build
```
Expected: Vite builds successfully, output in `web/dist/`.

- [ ] **Step 2: 构建 Go 后端（含 embed 前端）**

```bash
cd .. && go build ./cmd/router
```
Expected: exit 0, 生成 `router.exe`。

- [ ] **Step 3: 提交前端构建产物到 .gitignore（确认 dist 被忽略）**

```bash
git status
```
Expected: `web/dist/` 不在跟踪列表中。

- [ ] **Step 4: 运行全部后端测试确保无回归**

```bash
go test ./...
```
Expected: 所有包 PASS。

- [ ] **Step 5: 最终提交**

```bash
git add -A
git commit -m "feat(web): end-to-end build verification — Vite + Go embed"
```

---

## 自审

### 1. Spec 覆盖度

| Spec 要求 | 对应任务 |
|-----------|---------|
| 登录页 (POST /admin/login, JWT) | Task 4 |
| 仪表盘（统计卡片 + 饼图 + 柱状图） | Task 5 |
| API 源管理（表格 + CRUD + 测试连通） | Task 6 |
| 模型管理（表格 + CRUD + 设为判定） | Task 7 |
| 路由配置（表单 + Switch + InputNumber） | Task 8 |
| 请求日志（过滤 + 分页 + 展开 judge_raw） | Task 9 |
| 侧边栏导航布局 | Task 3 |
| 清爽青主题 | Task 2 |
| API 客户端（Axios + JWT 拦截器） | Task 2 |
| Go embed + SPA fallback | Task 10 |
| 开发 CORS | Task 10 |
| Vite proxy | Task 1 |
| 端到端构建 | Task 11 |

全部覆盖。

### 2. 占位符扫描

无 TBD、TODO 或占位符。所有步骤包含完整代码。

### 3. 类型一致性

- `Provider` 类型：`api/providers.ts` 定义 → Task 6, 7 使用，一致
- `Model` 类型：`api/models.ts` 定义 → Task 5, 7, 8 使用，一致
- `RoutingConfig` 类型：`api/routing.ts` 定义 → Task 8 使用，一致
- `RequestLog`, `Stats` 类型：`api/logs.ts` 定义 → Task 5, 9 使用，一致
- `LoginResponse` 类型：`api/auth.ts` 定义 → Task 4 使用，一致
- `App.ServeSPA(webFS fs.FS)` — Task 10 定义 → Task 10 main.go 调用，一致
