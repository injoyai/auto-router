package main

import (
	"io/fs"
	"log"
	"time"

	"auto-router/internal/config"
	"auto-router/internal/server"
	"auto-router/internal/store"
	"auto-router/web"
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
	if cfg.GatewayToken != "" {
		gwToken = cfg.GatewayToken
	}
	if cfg.AdminToken != "" {
		adminToken = cfg.AdminToken
	}
	app := server.NewApp(cfg, st, key, gwToken, adminToken)
	server.StartSessionCleanup(st, time.Minute)

	// Serve embedded React SPA (non-API requests fall back to index.html).
	if webSub, err := fs.Sub(web.FS, "dist"); err == nil {
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
