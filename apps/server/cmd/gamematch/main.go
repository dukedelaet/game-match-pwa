package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"gamematch/internal/config"
	"gamematch/internal/db"
	"gamematch/internal/game"
	"gamematch/internal/httpapi"
	"gamematch/internal/seed"
	"gamematch/internal/store"
)

func main() {
	cfg := config.Load()

	conn, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := db.Migrate(context.Background(), conn); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	st := store.New(conn, cfg)

	if len(os.Args) > 1 && os.Args[1] == "seed" {
		if err := seed.Run(context.Background(), st, cfg); err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Printf("seeded %s", cfg.DBPath())
		return
	}

	engine := game.NewEngine(st, cfg)
	matcher := game.NewMatcher(st, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.RunTicker(ctx, 5*time.Second)

	server := httpapi.NewServer(st, engine, matcher, cfg)
	log.Printf("gamematch listening on http://%s (data dir %s, debug=%t)", cfg.Addr, cfg.DataDir, cfg.Debug)
	if err := http.ListenAndServe(cfg.Addr, server.Router()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
