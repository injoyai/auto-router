# MySQL 数据库适配设计

## 背景

Auto Router 当前仅支持 SQLite（纯 Go 驱动 `github.com/glebarez/sqlite`）。需要适配 MySQL，**默认仍用 SQLite**，通过配置切换。

## 目标

- 新增 MySQL 驱动支持，通过配置切换
- SQLite 作为默认驱动，行为与现有完全一致
- 不改动任何 store 层查询代码（现有 SQL 均为 ANSI 标准，MySQL 原生兼容）
- 通过 Dialer 抽象封装驱动差异，便于未来扩展

## 现状分析

- `internal/store/store.go` 的 `Open(path)` 硬编码使用 `sqlite.Open(path)`
- SQLite 特有代码仅 2 处：`PRAGMA journal_mode=WAL`、`PRAGMA busy_timeout=5000`
- 所有查询使用的 SQL 函数（COALESCE/NULLIF/COUNT/SUM/LEFT JOIN）均为 ANSI 标准
- 表结构：Provider、Model、RoutingConfig、RequestLog、Setting、ModelGroup、ModelGroupItem
- 配置字段 `DBPath`，默认 `./data/database/auto-router.db`

## 设计

### 1. 配置结构扩展

**Config 新增字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `DBDriver` | `string` | 驱动类型：`sqlite`（默认）或 `mysql` |
| `DBDSN` | `string` | MySQL 连接串；SQLite 时留空，回退到 `DBPath` |

**配置优先级（保持现有）：** 环境变量 > 配置文件 > 默认值

**环境变量：**

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DB_DRIVER` | `sqlite` | 数据库驱动类型 |
| `DB_DSN` | (空) | MySQL 连接串（SQLite 时忽略） |
| `DB_PATH` | `./data/database/auto-router.db` | SQLite 文件路径（仅 SQLite 时使用） |

**YAML 配置示例：**

```yaml
db_driver: mysql
db_dsn: "root:password@tcp(127.0.0.1:3306)/auto_router?charset=utf8mb4&parseTime=true&loc=Local"
```

**选择逻辑：**

- `db_driver=sqlite`（默认）→ 用 `DB_PATH` 打开 SQLite，行为与现在完全一致
- `db_driver=mysql` → 用 `DB_DSN` 打开 MySQL

### 2. Dialer 抽象与驱动实现

**Dialer 接口（新文件 `internal/store/dialer.go`）：**

```go
// Dialer 封装特定数据库驱动的连接逻辑。
// 每个实现负责打开连接并完成驱动特定的初始化（如 SQLite 的 PRAGMA、MySQL 的连接池）。
type Dialer interface {
    Open(dsn string) (*gorm.DB, error)
}
```

**SQLiteDialer（新文件 `internal/store/sqlite_dialer.go`）：**

封装现有逻辑——目录创建、`sqlite.Open`、WAL/busy_timeout PRAGMA。`dsn` 即文件路径。

```go
type SQLiteDialer struct{}

func (SQLiteDialer) Open(path string) (*gorm.DB, error) {
    // 1. 确保父目录存在（:memory: 和无目录的相对路径跳过）
    // 2. gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
    // 3. PRAGMA journal_mode=WAL
    // 4. PRAGMA busy_timeout=5000
    // 返回 *gorm.DB
}
```

**MySQLDialer（新文件 `internal/store/mysql_dialer.go`）：**

```go
type MySQLDialer struct{}

func (MySQLDialer) Open(dsn string) (*gorm.DB, error) {
    // 1. gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
    // 2. db.DB() 获取底层 *sql.DB
    // 3. SetMaxOpenConns(50)
    // 4. SetMaxIdleConns(10)
    // 5. SetConnMaxLifetime(30 * time.Minute)
    // 返回 *gorm.DB
}
```

**store.Open 改造（`internal/store/store.go`）：**

```go
func Open(dialer Dialer, dsn string) (*Store, error) {
    db, err := dialer.Open(dsn)
    if err != nil {
        return nil, err
    }
    if err := db.AutoMigrate(&Provider{}, &Model{}, &RoutingConfig{}, &RequestLog{}, &Setting{}, &ModelGroup{}, &ModelGroupItem{}); err != nil {
        return nil, err
    }
    if err := db.FirstOrCreate(&RoutingConfig{}, RoutingConfig{ID: 1}).Error; err != nil {
        return nil, err
    }
    return &Store{DB: db}, nil
}
```

**main.go 改造：**

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
```

### 3. 关键设计决策

- **PRAGMA 隔离**：仅在 `SQLiteDialer.Open` 内执行，MySQL 不会触碰
- **连接池默认值**（50/10/30min）写死在 `MySQLDialer` 中，符合 YAGNI，后续需要再抽配置
- **目录创建逻辑**从 `store.Open` 下沉到 `SQLiteDialer.Open`，因为只有 SQLite 涉及文件路径
- **store.Open 只保留通用逻辑**：AutoMigrate + seed 单例行

### 4. 数据类型兼容性

- GORM `AutoMigrate` 在 MySQL 上自动建表，无需手写 DDL
- 字符串字段：GORM 默认映射为 MySQL `longtext`，无需指定 VARCHAR 长度
- `ModelGroup.Name` 有 `uniqueIndex`，MySQL utf8mb4 下 InnoDB 默认支持 3072 字节索引，Name 字段远小于此，无需特殊处理
- `time.Time`：MySQL DSN 必须带 `parseTime=true`，已在 DSN 示例中体现
- 现有 SQL（COALESCE/NULLIF/COUNT/SUM/LEFT JOIN）均为 ANSI 标准，MySQL 原生兼容，**无需改动任何 store 层查询代码**

### 5. 依赖管理（go.mod）

- 新增 `gorm.io/driver/mysql`（官方驱动，纯 Go，无 CGO）
- 保留 `github.com/glebarez/sqlite`（纯 Go SQLite）
- 两个驱动共存，互不冲突

### 6. 测试策略

- **现有测试**：所有测试用 SQLite `:memory:`，行为不变
- **store.Open 签名变更影响**：调用 `store.Open(path)` 的地方改为 `store.Open(store.SQLiteDialer{}, path)`
- **不新增 MySQL 集成测试**：需要 MySQL 实例，CI 复杂度高，YAGNI。手动验证即可
- **回归验证**：`go build ./...` + `go test ./...` 确保 SQLite 路径无回归

### 7. 文档更新

- `README.md` 配置章节新增 `DB_DRIVER`、`DB_DSN` 说明和 MySQL 配置示例
- `MEMORY.md` 记录多数据库支持决策

## 改动文件清单

| 文件 | 改动类型 | 说明 |
|------|----------|------|
| `internal/store/dialer.go` | 新增 | Dialer 接口定义 |
| `internal/store/sqlite_dialer.go` | 新增 | SQLiteDialer 实现（迁移现有逻辑） |
| `internal/store/mysql_dialer.go` | 新增 | MySQLDialer 实现 |
| `internal/store/store.go` | 修改 | Open 签名改为接收 Dialer；移除 SQLite 特有代码 |
| `internal/config/config.go` | 修改 | 新增 DBDriver、DBDSN 字段 |
| `cmd/main.go` | 修改 | 根据 DBDriver 选择 Dialer |
| `go.mod` | 修改 | 新增 gorm.io/driver/mysql 依赖 |
| `internal/store/store_test.go` | 修改 | 适配 Open 新签名 |
| 其他调用 store.Open 的测试文件 | 修改 | 适配 Open 新签名 |
| `README.md` | 修改 | 配置章节新增 MySQL 说明 |
| `MEMORY.md` | 修改 | 记录多数据库支持决策 |

## 非目标（YAGNI）

- 不支持 PostgreSQL 等其他数据库（未来需要时再加 Dialer 实现）
- 不提供从 SQLite 迁移数据到 MySQL 的工具
- 不新增 MySQL 集成测试（手动验证）
- 不抽离连接池配置为可配置项（使用合理默认值）
