package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr   string
	DBPath       string
	AdminToken   string // if empty, generated on first run and stored in DB
	GatewayToken string // if empty, generated on first run and stored in DB
	DevMode      bool
}

// fileConfig mirrors Config fields for the optional YAML config file.
// Password is an alias for AdminToken so the login password can be set with a
// more intuitive key; admin_token takes precedence when both are present.
type fileConfig struct {
	ListenAddr   string `yaml:"listen_addr"`
	DBPath       string `yaml:"db_path"`
	AdminToken   string `yaml:"admin_token"`
	Password     string `yaml:"password"`
	GatewayToken string `yaml:"gateway_token"`
	DevMode      bool   `yaml:"dev"`
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
		ListenAddr:   firstNonEmpty(os.Getenv("LISTEN_ADDR"), fc.ListenAddr, ":8080"),
		DBPath:       firstNonEmpty(os.Getenv("DB_PATH"), fc.DBPath, "./data/database/auto-router.db"),
		AdminToken:   firstNonEmpty(os.Getenv("ADMIN_TOKEN"), fc.AdminToken, fc.Password),
		GatewayToken: firstNonEmpty(os.Getenv("GATEWAY_TOKEN"), fc.GatewayToken),
		DevMode:      os.Getenv("DEV") != "" || fc.DevMode,
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
