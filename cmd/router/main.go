package main

import (
	"log"

	"auto-router/internal/config"
	"auto-router/internal/server"
)

func main() {
	cfg := config.Load()
	r := server.NewRouter(cfg)
	log.Printf("listening on %s", cfg.ListenAddr)
	if err := r.Run(cfg.ListenAddr); err != nil {
		log.Fatal(err)
	}
}
