# MySQL 数据库适配 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在保持 SQLite 默认行为不变的前提下，通过 Dialer 抽象新增 MySQL 驱动支持，由配置切换。

**Architecture:** 引入 `Dialer` 接口封装驱动差异（连接打开 + 驱动特定初始化），`store.Open` 改为接收 `Dialer` 实例。`SQLiteDialer` 迁移现有目录创建 + PRAGMA 逻辑，`MySQLDialer` 负责连接池调优。store 层查询代码零改动（现有 SQL 均为 ANSI 标准）。

**Tech Stack:** Go 1.25.4 + GORM + `github.com/glebarez/sqlite`（纯 Go SQLite，保留）+ `gorm.io/driver/mysql`（新增，纯 Go，无 CGO）

**Spec:** `docs/superpowers/specs/2026-07-31-mysql-database-support-design.md`

---

## File Structure

| 文件 | 改动 | 职责 |
|------|------|------|
| `internal/config/config.go` | 修改 | 新增 `DBDriver`、`DBDSN` 字段及对应 env/yaml 加载 |
| `internal/store/dialer.go` | 新增 | `Dialer` 接口定义 |
| `internal/store/sqlite_dialer.go` | 新增 | `SQLiteDialer`：目录创建 + `sqlite.Open` + WAL/busy_timeout PRAGMA |
| `internal/store/mysql_dialer.go` | 新增 | `MySQLDialer`：`mysql.Open` + 连接池调优 |
| `internal/store/store.go` | 修改 | `Open(dialer Dialer, dsn string)`；移除 SQLite 特有代码，仅保留 AutoMigrate + seed |
| `cmd/main.go` | 修改 | 根据 `cfg.DBDriver` 选择 Dialer 与 dsn |
| `go.mod` / `go.sum` | 修改 | 新增 `gorm.io/driver/mysql` 依赖 |
| `internal/store/store_test.go` | 修改 | 适配 `Open` 新签名 |
| `internal/server/bootstrap_test.go` | 修改 | 适配 `Open` 新签名 |
| `internal/server/apptest_test.go` | 修改 | 适配 `Open` 新签名（2 处） |
| `internal/server/gateway_test.go` | 修改 | 适配 `Open` 新签名 |
| `README.md` | 修改 | 配置章节新增 `DB_DRIVER`/`DB_DSN` 及 MySQL 示例 |
| `MEMORY.md` | 修改 | 记录多数据库支持决策 |

---

### Task 1: 扩展 Config 新增 DBDriver 与 DBDSN 字段

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: 给 Config 结构体新增 DBDriver 与 DBDSN 字段**

在 `internal/config/config.go` 中，将 `DBDriver` 和 `DBDSN` 加入 `Config` 结构体（放在 `DBPath` 之后，保持数据库相关字段聚集）：

```go
type Config struct {
	ListenAddr   string
	DBDriver     string // sqlite (默认) | mysql
	DBPath       string // SQLite 文件路径（仅 sqlite 驱动使用）
	DBDSN        string // MySQL 连接串（仅 mysql 驱动使用）
	Password     string // admin login password; if empty, generated on first run and stored in DB
	GatewayToken string // if empty, generated on first run and stored in DB
	DevMode      bool
}
```

- [ ] **Step 2: 给 fileConfig 结构体新增对应 yaml 标签字段**

```go
type fileConfig struct {
	ListenAddr   string `yaml:"listen_addr"`
	DBDriver     string `yaml:"db_driver"`
	DBPath       string `yaml:"db_path"`
	DBDSN        string `yaml:"db_dsn"`
	Password     string `yaml:"password"`
	GatewayToken string `yaml:"gateway_token"`
	DevMode      bool   `yaml:"dev"`
}
```

- [ ] **Step 3: 在 Load() 中加载新字段**

修改 `Load()` 的返回值，新增 `DBDriver`（默认 `sqlite`）和 `DBDSN`（默认空）：

```go
return Config{
	ListenAddr:   firstNonEmpty(os.Getenv("LISTEN_ADDR"), fc.ListenAddr, ":8080"),
	DBDriver:     firstNonEmpty(os.Getenv("DB_DRIVER"), fc.DBDriver, "sqlite"),
	DBPath:       firstNonEmpty(os.Getenv("DB_PATH"), fc.DBPath, "./data/database/auto-router.db"),
	DBDSN:        firstNonEmpty(os.Getenv("DB_DSN"), fc.DBDSN),
	Password:     firstNonEmpty(os.Getenv("PASSWORD"), fc.Password),
	GatewayToken: firstNonEmpty(os.Getenv("GATEWAY_TOKEN"), fc.GatewayToken),
	DevMode:      os.Getenv("DEV") != "" || fc.DevMode,
}, nil
```

- [ ] **Step 4: 编译验证**

Run: `go build ./...`
Expected: 编译通过，无报错（此阶段新字段未被引用，但不会报错）。

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add DBDriver and DBDSN fields for multi-db support"
```

---

### Task 2: 引入 Dialer 抽象并重构 store.Open

本任务将驱动差异从 `store.Open` 中剥离：新建 `Dialer` 接口与 `SQLiteDialer` 实现，`store.Open` 改为接收 `Dialer`。由于 `Open` 签名变更会破坏所有调用方，同任务内一并适配全部测试，保证最终 `go test ./...` 绿色。

**Files:**
- Create: `internal/store/dialer.go`
- Create: `internal/store/sqlite_dialer.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify: `internal/server/bootstrap_test.go`
- Modify: `internal/server/apptest_test.go`
- Modify: `internal/server/gateway_test.go`

- [ ] **Step 1: 创建 Dialer 接口定义文件**

创建 `internal/store/dialer.go`：

```go
package store

import "gorm.io/gorm"

// Dialer 封装特定数据库驱动的连接逻辑。
// 每个实现负责打开连接并完成驱动特定的初始化
// （如 SQLite 的 PRAGMA、MySQL 的连接池调优）。
type Dialer interface {
	Open(dsn string) (*gorm.DB, error)
}
```

- [ ] **Step 2: 创建 SQLiteDialer（迁移现有逻辑）**

创建 `internal/store/sqlite_dialer.go`，将 `store.go` 中的目录创建、`sqlite.Open`、WAL/busy_timeout 逻辑原样搬入：

```go
package store

import (
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQLiteDialer 使用纯 Go 驱动 github.com/glebarez/sqlite 打开 SQLite 数据库。
// dsn 即 SQLite 文件路径（":memory:" 表示内存库）。
type SQLiteDialer struct{}

func (SQLiteDialer) Open(path string) (*gorm.DB, error) {
	// Ensure the parent directory exists for file-backed databases. Skipped
	// for ":memory:" (no directory) and for relative paths without a dir.
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	// I8: enable WAL + a 5s busy_timeout so concurrent readers/writers on
	// file-backed databases don't immediately fail with "database is locked".
	// These are no-ops on :memory: databases (per-connection, no journal) but
	// never error, so they are safe to apply unconditionally and before any
	// migration/write.
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
		return nil, err
	}
	return db, nil
}
```

- [ ] **Step 3: 重构 store.go，Open 改为接收 Dialer**

将 `internal/store/store.go` 整体替换为如下内容（移除 SQLite 特有 import 与逻辑，仅保留 AutoMigrate + seed）：

```go
package store

import "gorm.io/gorm"

type Store struct {
	DB *gorm.DB
}

// Open 使用给定 Dialer 打开数据库并完成通用初始化
// （AutoMigrate + seed routing_config 单例行）。
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
	return &Store{DB: db}, nil
}
```

- [ ] **Step 4: 适配 internal/store/store_test.go**

将 `newTestStore` 中的 `Open(":memory:")` 改为 `Open(SQLiteDialer{}, ":memory:")`：

```go
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(SQLiteDialer{}, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}
```

- [ ] **Step 5: 适配 internal/server/bootstrap_test.go**

将第 12 行 `store.Open(":memory:")` 改为 `store.Open(store.SQLiteDialer{}, ":memory:")`：

```go
st, err := store.Open(store.SQLiteDialer{}, ":memory:")
```

- [ ] **Step 6: 适配 internal/server/apptest_test.go（2 处）**

将 `newTestApp`（第 21 行）和 `newTestAppWithProtocol`（第 66 行）中的 `store.Open(":memory:")` 均改为 `store.Open(store.SQLiteDialer{}, ":memory:")`：

```go
st, err := store.Open(store.SQLiteDialer{}, ":memory:")
```

- [ ] **Step 7: 适配 internal/server/gateway_test.go**

将第 114 行 `store.Open(":memory:")` 改为 `store.Open(store.SQLiteDialer{}, ":memory:")`：

```go
st, err := store.Open(store.SQLiteDialer{}, ":memory:")
```

- [ ] **Step 8: 编译验证**

Run: `go build ./...`
Expected: 编译通过。若报错，检查是否有遗漏的 `store.Open(` 旧签名调用。

- [ ] **Step 9: 回归测试**

Run: `go test ./...`
Expected: 全部测试通过（SQLite 路径行为与重构前完全一致）。

- [ ] **Step 10: Commit**

```bash
git add internal/store/dialer.go internal/store/sqlite_dialer.go internal/store/store.go internal/store/store_test.go internal/server/bootstrap_test.go internal/server/apptest_test.go internal/server/gateway_test.go
git commit -m "refactor(store): introduce Dialer abstraction, extract SQLiteDialer"
```

---

### Task 3: 新增 MySQL 驱动依赖与 MySQLDialer

**Files:**
- Modify: `go.mod` / `go.sum`（由 `go get` 自动更新）
- Create: `internal/store/mysql_dialer.go`

- [ ] **Step 1: 添加 gorm.io/driver/mysql 依赖**

Run: `go get gorm.io/driver/mysql`
Expected: 命令成功，`go.mod` 新增 `gorm.io/driver/mysql` 及其间接依赖，`go.sum` 同步更新。

- [ ] **Step 2: 创建 MySQLDialer**

创建 `internal/store/mysql_dialer.go`：

```go
package store

import (
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// MySQLDialer 使用官方纯 Go 驱动 gorm.io/driver/mysql 打开 MySQL 数据库。
// dsn 为标准 MySQL 连接串，需包含 parseTime=true 以正确映射 time.Time。
type MySQLDialer struct{}

func (MySQLDialer) Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}
```

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: 编译通过（MySQLDialer 此时未被引用，但作为独立文件应能编译）。

- [ ] **Step 4: 回归测试**

Run: `go test ./...`
Expected: 全部测试通过（未引入 MySQL 集成测试，SQLite 路径不受影响）。

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/store/mysql_dialer.go
git commit -m "feat(store): add MySQLDialer with connection pool tuning"
```

---

### Task 4: 在 main.go 中根据配置选择 Dialer

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: 修改 main.go，按 DBDriver 选择 Dialer 与 dsn**

将 `cmd/main.go` 中第 18 行 `st, err := store.Open(cfg.DBPath)` 替换为如下选择逻辑：

```go
	var dialer store.Dialer
	dsn := cfg.DBPath
	if cfg.DBDriver == "mysql" {
		dialer = store.MySQLDialer{}
		dsn = cfg.DBDSN
	} else {
		dialer = store.SQLiteDialer{}
	}
	st, err := store.Open(dialer, dsn)
	if err != nil {
		log.Fatal(err)
	}
```

完整上下文（替换后 main 函数开头部分）：

```go
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	var dialer store.Dialer
	dsn := cfg.DBPath
	if cfg.DBDriver == "mysql" {
		dialer = store.MySQLDialer{}
		dsn = cfg.DBDSN
	} else {
		dialer = store.SQLiteDialer{}
	}
	st, err := store.Open(dialer, dsn)
	if err != nil {
		log.Fatal(err)
	}
	key, gwToken, adminToken, err := server.Bootstrap(st)
	if err != nil {
		log.Fatal(err)
	}
	// ... 后续代码不变
```

注意：原 `st, err := store.Open(cfg.DBPath)` 行后的 `if err != nil { log.Fatal(err) }` 已包含在上面的代码块中，不要重复保留。

- [ ] **Step 2: 编译验证**

Run: `go build ./...`
Expected: 编译通过。

- [ ] **Step 3: 回归测试**

Run: `go test ./...`
Expected: 全部测试通过。

- [ ] **Step 4: Commit**

```bash
git add cmd/main.go
git commit -m "feat(main): select Dialer based on DBDriver config"
```

---

### Task 5: 更新 README.md 与 MEMORY.md 文档

**Files:**
- Modify: `README.md`
- Modify: `MEMORY.md`

- [ ] **Step 1: 在 README.md 环境变量表格中新增 DB_DRIVER 与 DB_DSN**

在 `README.md` 的环境变量表格中，`DB_PATH` 行之前插入 `DB_DRIVER` 行，`DB_PATH` 行之后插入 `DB_DSN` 行：

```markdown
| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8080` | 监听地址 |
| `DB_DRIVER` | `sqlite` | 数据库驱动：`sqlite` 或 `mysql` |
| `DB_PATH` | `./data/database/auto-router.db` | SQLite 文件路径（仅 `sqlite` 驱动使用） |
| `DB_DSN` | (空) | MySQL 连接串（仅 `mysql` 驱动使用，需含 `parseTime=true`） |
| `GATEWAY_TOKEN` | 自动生成 | 客户端访问网关的 token(可覆盖) |
| `PASSWORD` | 自动生成 | 管理后台登录密码(可覆盖) |
| `CONFIG_FILE` | `./config/config.yaml` | 配置文件路径(不存在则忽略) |
| `DEV` | (未设置) | 任意非空值开启开发模式(CORS 放开) |
```

- [ ] **Step 2: 在 README.md YAML 配置示例中新增 db_driver / db_dsn**

将 YAML 示例更新为：

```yaml
listen_addr: ":8080"
db_driver: "sqlite"                 # sqlite (默认) 或 mysql
db_path: "./data/database/auto-router.db"  # 仅 sqlite 驱动使用
db_dsn: ""                          # 仅 mysql 驱动使用，例如：
# db_dsn: "root:password@tcp(127.0.0.1:3306)/auto_router?charset=utf8mb4&parseTime=true&loc=Local"
password: "your-admin-password"   # 管理后台登录密码
gateway_token: "your-gateway-token"
dev: false
```

- [ ] **Step 3: 更新 MEMORY.md 技术栈行与新增决策记录**

在 `MEMORY.md` 的「技术栈」章节，将 SQLite 行更新为多数据库支持描述：

```markdown
## 技术栈

- **后端**: Go 1.25 + Gin + GORM + 多数据库支持 (SQLite 默认 via glebarez/sqlite 纯 Go 驱动; MySQL via gorm.io/driver/mysql)
- **前端**: React 18 + TypeScript + Ant Design 5 + Vite + TanStack Query
- **设计系统**: "Frosted Botanical" - 毛玻璃植物风，定义在 `web/src/global.css`
```

在「关键决策」列表末尾新增第 5 条：

```markdown
5. **多数据库支持**: 通过 `Dialer` 接口抽象驱动差异（`internal/store/dialer.go`）。默认 SQLite，通过 `DB_DRIVER=mysql` 切换 MySQL（DSN 由 `DB_DSN` 提供）。`store.Open(dialer, dsn)` 仅保留通用逻辑（AutoMigrate + seed），驱动特定初始化（SQLite PRAGMA、MySQL 连接池）在各 Dialer 实现内。现有 SQL 均为 ANSI 标准，store 层查询代码零改动
```

在「项目结构」的 `store/` 行后补充 Dialer 文件说明：

```markdown
  store/                     # GORM 数据层 (Provider, Model, ModelGroup, RequestLog, RoutingConfig, Setting)
                             # dialer.go: Dialer 接口; sqlite_dialer.go / mysql_dialer.go: 驱动实现
```

- [ ] **Step 4: Commit**

```bash
git add README.md MEMORY.md
git commit -m "docs: document MySQL support and DBDriver/DBDSN config"
```

---

## 验收清单

完成全部任务后，执行以下整体验证：

- [ ] **`go build ./...`** 编译通过
- [ ] **`go test ./...`** 全部测试通过（SQLite 路径无回归）
- [ ] **`go vet ./...`** 无警告
- [ ] 默认行为不变：不设置 `DB_DRIVER` 时，仍使用 SQLite + `DB_PATH`，WAL/busy_timeout 生效
- [ ] MySQL 路径可编译：`MySQLDialer` 类型存在且实现 `Dialer` 接口
- [ ] `README.md` 环境变量表与 YAML 示例包含 `DB_DRIVER` / `DB_DSN`
- [ ] `MEMORY.md` 技术栈与关键决策已更新
