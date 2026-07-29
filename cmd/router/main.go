package main

import (
	"log"
	"time"

	"auto-router/internal/config"
	"auto-router/internal/server"
	"auto-router/internal/store"
)

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
	// env overrides take precedence over persisted/generated values
	if cfg.GatewayToken != "" {
		gwToken = cfg.GatewayToken
	}
	if cfg.AdminToken != "" {
		adminToken = cfg.AdminToken
	}
	app := server.NewApp(cfg, st, key, gwToken, adminToken)
	server.StartSessionCleanup(st, time.Minute)
	log.Printf("listening on %s | gateway token: %s | admin token: %s", cfg.ListenAddr, gwToken, adminToken)
	if err := app.Router.Run(cfg.ListenAddr); err != nil {
		log.Fatal(err)
	}
}
