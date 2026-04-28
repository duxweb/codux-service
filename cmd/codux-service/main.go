package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/duxweb/codux-service/internal/server"
	"github.com/duxweb/codux-service/internal/store"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	addr := flag.String("addr", env("CODEX_SERVER_ADDR", ":8088"), "HTTP/WebSocket listen address")
	dbPath := flag.String("db", env("CODEX_SERVER_DB", "codux-service.sqlite3"), "SQLite database path")
	pairingTTLSeconds := flag.Int(
		"pairing-ttl", envInt("CODEX_PAIRING_TTL", 300), "pairing QR lifetime in seconds")
	flag.Parse()
	pairingTTL := time.Duration(*pairingTTLSeconds) * time.Second

	database, err := store.Open(*dbPath)
	if err != nil {
		logger.Error("open database failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	hub := server.NewHub(database, logger, pairingTTL)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           hub.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("codux relay listening", "addr", *addr, "db", *dbPath, "version", version)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub.Close()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return fallback
	}
	return number
}
