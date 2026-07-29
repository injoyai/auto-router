# Auto Model Router — 前端管理后台设计文档

- 日期: 2026-07-29
- 状态: 已批准（待实现）
- 技术栈: React + Vite + Ant Design 5 + TanStack Query + Axios
- 图表: @ant-design/charts
- 部署: Go embed（Vite 构建产物嵌入 Go 二进制）

## 1. 概述

为 auto-router 后端增加一个基于 React 的管理后台前端，用户可通过浏览器管理 API 源、模型、路由配置，并查看仪表盘和请求日志。

### 1.1 核心目标

- **登录认证**：admin token 换取 JWT，JWT 过期自动跳转登录页
- **仪表盘**：统计卡片（总请求数、成功率、活跃模型数、平均延迟）+ 饼图（路由原因分布）+ 柱状图（模型使用占比）
- **API 源管理**：增删改查 + 测试连通、根据协议类型（openai/claude）配置
- **模型管理**：增删改查 + 设为判定模型、关联 Provider
- **路由配置**：选择判定模型/默认模型 + 开关 next_model 指令 + TTL/截断参数
- **请求日志**：分页 + 过滤（按路由原因/模型）+ 查看判定模型原始返回

### 1.2 非目标（YAGNI）

- 不做多用户/权限体系（单 admin JWT）
- 不做实时 WebSocket 推送
- 不做前端自动化测试
- 不做暗色模式（仅清爽青浅色主题）
- 不做国际化 i18n

## 2. 视觉设计

### 2.1 布局：侧边栏导航

- 左侧固定白色侧边栏（宽度 220px），列出 5 个页面 + 底部"退出登录"
- 右侧为内容区，Ant Design Layout 组件实现
- 侧边栏顶部显示产品名称 "🤖 Auto Router"

### 2.2 配色：清爽青

基于 Ant Design 5 ConfigProvider theme token 覆盖：

| Token | 值 | 用途 |
|-------|-----|------|
| `colorPrimary` | `#13c2c2` | 主色（按钮、链接、选中态） |
| `colorLink` | `#08979c` | 链接色 |
| `colorBgContainer` | `#ffffff` | 侧边栏背景 |
| `colorBgLayout` | `#f5f5f5` | 内容区背景 |
| `borderRadius` | `6` | 圆角 |

- 侧边栏选中项：浅青底 `#e6fffb` + 青文字 `#08979c`
- 路由原因 Tag 颜色：override=蓝、session=绿、judge=青、fallback=橙

## 3. 页面详情

### 3.1 登录页 (`/login`)

- 居中卡片（max-width 400px），黑色背景页面
- 标题 "Auto Router 管理后台"
- 密码输入框 + "登录" 按钮
- 调用 `POST /admin/login`，成功存 JWT → 跳转 `/`
- 失败显示 Ant Design Alert type="error"

### 3.2 仪表盘 (`/`)

- **4 个 StatisticCard**（@ant-design/pro-card 可选，或直接用 Card+Statistic）：
  - 总请求数
  - 成功率（status < 400 的占比，百分比）
  - 活跃模型数（enabled 的模型数量）
  - 平均延迟（ms）
- **饼图**：路由原因分布（override / session / judge / fallback），来源于 `/admin/stats` 的 `by_reason` 数组
- **柱状图**：模型使用占比（Top N 模型的路由次数），对日志按 routed_model 聚合（前端本地聚合或后端已有数据）

数据来源：`GET /admin/stats`

### 3.3 API 源管理 (`/providers`)

- **Table** 列：名称、Base URL、协议（openai/claude Tag）、启用状态（Switch）、操作
- **新增/编辑**：Modal 表单，字段：
  - 名称（Input, required）
  - Base URL（Input, required, placeholder: "https://api.openai.com/v1"）
  - API Key（Input.Password, required）
  - 协议（Select: openai / claude）
  - 启用（Switch, default: true）
- **测试连通**：行操作按钮 → 调用 `POST /admin/providers/:id/test` → Modal 显示结果（ok=true 绿色 / ok=false 红色 + error 文本）
- **删除**：Popconfirm 确认，若 409 "in use" 则 message.warning 提示

### 3.4 模型管理 (`/models`)

- **Table** 列：名称、显示名、Provider、描述、判定模型（Tag "判定"）、启用（Switch）、操作
- is_judge=true 的行高亮浅青背景
- **新增/编辑**：Modal 表单，字段：
  - 名称（Input, required）
  - 显示名（Input, required）
  - Provider（Select, 从已启用 Provider 列表中选）
  - 描述（Input.TextArea）
  - 启用（Switch, default: true）
- **设为判定模型**：行操作按钮 → `POST /admin/models/:id/judge` → refetch models 列表刷新高亮
- **删除**：Popconfirm 确认，同 Provider 的 409 处理

### 3.5 路由配置 (`/routing`)

- 页面级 Form（非弹窗），Card 包裹
- 字段：
  - 判定模型（Select, 从已启用模型列表选，支持清除）
  - 默认兜底模型（Select, 从已启用模型列表选，支持清除）
  - Enable Next Model Directive（Switch）+ Tooltip 说明
  - Session TTL（InputNumber, min=60, max=86400, suffix="秒"）
  - Judge Max Input Chars（InputNumber, min=100, max=10000, suffix="字符"）
- 保存按钮 → `PUT /admin/routing` → message.success

### 3.6 请求日志 (`/logs`)

- **过滤栏**（Table 上方）：
  - 路由原因（Select: 全部/override/session/judge/fallback）
  - 模型名（Input.Search）
  - 查询按钮 + 重置按钮
- **Table** 列：
  - 时间（created_at, 格式化为本地时间）
  - 会话ID（session_id, 可能为空显示 "-"）
  - 请求模型（requested_model）
  - 路由模型（routed_model）
  - 路由原因（Tag 彩色标签）
  - 状态码（status, 200 绿色 / 错误 红色）
  - 延迟（latency_ms + "ms"）
  - 错误（error, 有则显示红色文本，无则 "-"）
- **展开行**：点击行可展开查看 judge_raw（判定模型原始返回），等宽字体 pre 块
- **分页**：Ant Design Pagination，默认 page_size=50

## 4. API 客户端

### 4.1 Axios 实例 (`api/client.ts`)

```ts
const apiClient = axios.create({ baseURL: "" })
// 请求拦截器：附加 Authorization header
apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem("admin_jwt")
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})
// 响应拦截器：401 清除 JWT → 跳转 /login
apiClient.interceptors.response.use(
  (resp) => resp,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem("admin_jwt")
      window.location.href = "/login"
    }
    return Promise.reject(err)
  }
)
```

### 4.2 API 模块

- `api/auth.ts`: `login(token: string) → { token, expires_in }`
- `api/providers.ts`: `listProviders()`, `createProvider()`, `updateProvider()`, `deleteProvider()`, `testProvider(id)`
- `api/models.ts`: `listModels()`, `createModel()`, `updateModel()`, `deleteModel()`, `setJudgeModel(id)`
- `api/routing.ts`: `getRoutingConfig()`, `updateRoutingConfig()`
- `api/logs.ts`: `listLogs(params)`, `getStats()`

### 4.3 TanStack Query Hooks

每个 API 调用封装为对应的 `useQuery` / `useMutation` hook：
```ts
// 示例
export function useProviders() {
  return useQuery({ queryKey: ["providers"], queryFn: listProviders })
}
export function useCreateProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: createProvider,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["providers"] }),
  })
}
```

## 5. 前端路由

```tsx
<BrowserRouter>
  <Routes>
    <Route path="/login" element={<Login />} />
    <Route path="/" element={<Layout />}>
      <Route index element={<Dashboard />} />
      <Route path="providers" element={<Providers />} />
      <Route path="models" element={<Models />} />
      <Route path="routing" element={<Routing />} />
      <Route path="logs" element={<Logs />} />
    </Route>
  </Routes>
</BrowserRouter>
```

- `/login`：无 Layout 壳，独立全屏页面
- `/` 及子路由：共享 Layout 壳（侧边栏 + 内容区），`<Outlet />` 渲染子页面

## 6. 后端改动

### 6.1 Go embed 静态文件 (`cmd/router/main.go`)

```go
import "embed"

//go:embed web/dist/*
var webFS embed.FS

// ...
webSub, _ := fs.Sub(webFS, "web/dist")
r.NoRoute(func(c *gin.Context) {
    // 非 API 路径返回 SPA index.html
    path := c.Request.URL.Path
    if !strings.HasPrefix(path, "/v1") && !strings.HasPrefix(path, "/admin") && path != "/health" {
        // 尝试静态文件，找不到则 fallback 到 index.html
        f, err := webSub.Open(strings.TrimPrefix(path, "/"))
        if err != nil {
            c.FileFromFS("/", http.FS(webSub))
            return
        }
        f.Close()
        c.FileFromFS(path, http.FS(webSub))
        return
    }
    c.JSON(404, gin.H{"error": "not found"})
})
```

### 6.2 开发模式 CORS

开发时 Vite 跑在 `localhost:5173`，Go 跑在 `localhost:8080`，需要 CORS。不加在生产代码中，仅开发时通过环境变量或 `--dev` flag 启用。

## 7. 文件结构

```
web/
├── package.json
├── vite.config.ts
├── tsconfig.json
├── tsconfig.node.json
├── index.html
└── src/
    ├── main.tsx
    ├── App.tsx
    ├── vite-env.d.ts
    ├── api/
    │   ├── client.ts
    │   ├── auth.ts
    │   ├── providers.ts
    │   ├── models.ts
    │   ├── routing.ts
    │   └── logs.ts
    ├── pages/
    │   ├── Login.tsx
    │   ├── Dashboard.tsx
    │   ├── Providers.tsx
    │   ├── Models.tsx
    │   ├── Routing.tsx
    │   └── Logs.tsx
    ├── components/
    │   └── Layout.tsx
    └── theme.ts
```

## 8. 构建与部署

### 8.1 开发

```bash
# 终端 1: Go 后端
go run ./cmd/router --dev

# 终端 2: Vite dev server（proxy → Go 后端）
cd web && npm run dev
```

`vite.config.ts` proxy 配置：
```ts
export default defineConfig({
  server: {
    proxy: {
      "/v1": "http://localhost:8080",
      "/admin": "http://localhost:8080",
      "/health": "http://localhost:8080",
    },
  },
})
```

### 8.2 生产构建

```bash
cd web && npm run build    # → web/dist
cd .. && go build ./cmd/router  # embed web/dist → 单二进制 router.exe
./router.exe                # 直接运行，无需外部 web server
```

## 9. 依赖

```json
{
  "dependencies": {
    "react": "^18.3",
    "react-dom": "^18.3",
    "react-router-dom": "^6",
    "antd": "^5",
    "@ant-design/charts": "^1",
    "@ant-design/icons": "^5",
    "axios": "^1",
    "@tanstack/react-query": "^5"
  },
  "devDependencies": {
    "typescript": "^5",
    "vite": "^5",
    "@vitejs/plugin-react": "^4",
    "@types/react": "^18",
    "@types/react-dom": "^18"
  }
}
```

## 10. 决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 布局 | 侧边栏导航 | Ant Design Pro 经典布局，多页面切换清晰 |
| 配色 | 清爽青 #13c2c2 | 用户偏好，清爽现代，长时间使用不疲劳 |
| 图表库 | @ant-design/charts | 与 Ant Design 视觉无缝融合，饼图/柱状图开箱即用 |
| 状态管理 | TanStack Query only | 无额外 store，服务端状态天然适合 React Query |
| 部署 | Go embed 单文件 | 设计文档既定方案，零外部依赖 |
| 测试 | 不做前端测试 | 5 页面管理后台，手工验证即可，YAGNI |
| 无暗色模式 | 仅浅色 | 管理后台使用频率不高，保持简洁 |
