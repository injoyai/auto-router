package main

import (
	"io/fs"
	"log"

	"auto-router/internal/config"
	"auto-router/internal/server"
	"auto-router/internal/store"
	"auto-router/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(store.SQLiteDialer{}, cfg.DBPath)
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
	if cfg.Password != "" {
		adminToken = cfg.Password
	}
	app := server.NewApp(cfg, st, key, gwToken, adminToken)

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
