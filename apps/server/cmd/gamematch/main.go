package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gamematch/internal/config"
	"gamematch/internal/db"
	"gamematch/internal/game"
	"gamematch/internal/httpapi"
	"gamematch/internal/push"
	"gamematch/internal/seed"
	"gamematch/internal/store"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config: %v", err)
	}

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

	if len(os.Args) > 1 && os.Args[1] == "vapid" {
		privateKey, publicKey, err := push.GenerateKeys()
		if err != nil {
			log.Fatalf("vapid: %v", err)
		}
		fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", publicKey, privateKey)
		return
	}

	engine := game.NewEngine(st, cfg)
	matcher := game.NewMatcher(st, cfg)

	// SIGTERM is how systemd stops the service, which is how every deploy
	// happens. Without a handler the process dies mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go engine.RunTicker(ctx, 5*time.Second)

	srv := newHTTPServer(cfg.Addr, httpapi.NewServer(st, engine, matcher, cfg).Router())

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		log.Printf("shutdown: draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("gamematch listening on http://%s (data dir %s, debug=%t)", cfg.Addr, cfg.DataDir, cfg.Debug)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}

	// Wait for in-flight requests so the database is not closed underneath them.
	select {
	case <-shutdownDone:
	case <-time.After(shutdownGrace):
		log.Printf("shutdown: timed out waiting for drain")
	}
}

// shutdownGrace bounds how long a restart may spend draining.
const shutdownGrace = 25 * time.Second

// newHTTPServer builds the HTTP server with the timeouts it was missing.
// Without them the zero values mean unlimited, so a handful of connections
// that never finish their headers hold goroutines indefinitely.
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: h,
		// Bounds the window between accepting a connection and receiving the
		// request headers, which is what a slow-loris attack holds open.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
