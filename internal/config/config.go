package config

import "os"

type Config struct {
	ListenAddr   string
	DBPath       string
	AdminToken   string // if empty, generated on first run and stored in DB
	GatewayToken string // if empty, generated on first run and stored in DB
}

func Load() Config {
	c := Config{
		ListenAddr:   getEnv("LISTEN_ADDR", ":8080"),
		DBPath:       getEnv("DB_PATH", "auto-router.db"),
		AdminToken:   os.Getenv("ADMIN_TOKEN"),
		GatewayToken: os.Getenv("GATEWAY_TOKEN"),
	}
	return c
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
