package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 顶层配置，按职责分为 Server / DB / Auth 三组。
type Config struct {
	Server ServerConfig
	DB     DBConfig
	Auth   AuthConfig
}

// ServerConfig 服务运行相关配置。
type ServerConfig struct {
	ListenAddr string
	DevMode    bool
}

// DBConfig 数据库相关配置。
type DBConfig struct {
	Driver string // sqlite (默认) | mysql
	Path   string // SQLite 文件路径（仅 sqlite 驱动使用）
	DSN    string // MySQL 连接串（仅 mysql 驱动使用，需含 parseTime=true）
}

// AuthConfig 认证相关配置。
type AuthConfig struct {
	Password     string // admin login password; if empty, generated on first run and stored in DB
	GatewayToken string // if empty, generated on first run and stored in DB
}

// fileConfig mirrors Config structure for the optional YAML config file.
// 字段名与 yaml 键均为分组嵌套，不兼容旧版扁平格式。
type fileConfig struct {
	Server struct {
		ListenAddr string `yaml:"listen_addr"`
		DevMode    bool   `yaml:"dev"`
	} `yaml:"server"`
	DB struct {
		Driver string `yaml:"driver"`
		Path   string `yaml:"path"`
		DSN    string `yaml:"dsn"`
	} `yaml:"db"`
	Auth struct {
		Password     string `yaml:"password"`
		GatewayToken string `yaml:"gateway_token"`
	} `yaml:"auth"`
}

// Load builds Config with precedence: env var > config file > default.
// The config file path is taken from the CONFIG_FILE env var, defaulting to
// ./config/config.yaml relative to the working directory. A missing file is
// not an error.
func Load() (Config, error) {
	fc, err := loadFile(configFilePath())
	if err != nil {
		return Config{}, err
	}
	return Config{
		Server: ServerConfig{
			ListenAddr: firstNonEmpty(os.Getenv("SERVER_LISTEN_ADDR"), fc.Server.ListenAddr, ":8080"),
			DevMode:    os.Getenv("SERVER_DEV") != "" || fc.Server.DevMode,
		},
		DB: DBConfig{
			Driver: firstNonEmpty(os.Getenv("DB_DRIVER"), fc.DB.Driver, "sqlite"),
			Path:   firstNonEmpty(os.Getenv("DB_PATH"), fc.DB.Path, "./data/database/auto-router.db"),
			DSN:    firstNonEmpty(os.Getenv("DB_DSN"), fc.DB.DSN),
		},
		Auth: AuthConfig{
			Password:     firstNonEmpty(os.Getenv("AUTH_PASSWORD"), fc.Auth.Password),
			GatewayToken: firstNonEmpty(os.Getenv("AUTH_GATEWAY_TOKEN"), fc.Auth.GatewayToken),
		},
	}, nil
}

func configFilePath() string {
	if p := os.Getenv("CONFIG_FILE"); p != "" {
		return p
	}
	return "./config/config.yaml"
}

func loadFile(path string) (fileConfig, error) {
	var fc fileConfig
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fc, nil // no config file is fine
		}
		return fc, fmt.Errorf("read config file %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return fc, fmt.Errorf("parse config file %s: %w", path, err)
	}
	return fc, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
