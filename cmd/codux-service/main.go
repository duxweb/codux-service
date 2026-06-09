package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/duxweb/codux-service/internal/server"
	"github.com/duxweb/codux-service/internal/store"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	config, err := loadRuntimeConfig(os.Args[1:])
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}

	database, err := store.Open(config.DBPath)
	if err != nil {
		logger.Error("open database failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	hub := server.NewHub(database, logger, config.PairingTTL)
	var stats *server.StatsRecorder
	if config.StatsEnabled {
		stats, err = server.NewStatsRecorder(config.StatsPath, config.StatsFlushInterval, logger)
		if err != nil {
			logger.Error("open stats log failed", "path", config.StatsPath, "error", err)
			os.Exit(1)
		}
		defer stats.Close()
		hub.SetStatsRecorder(stats)
	}
	httpServer := &http.Server{
		Addr:              config.Addr,
		Handler:           hub.Routes(),
		ReadHeaderTimeout: config.ReadHeaderTimeout,
	}

	go func() {
		logger.Info(
			"codux relay listening",
			"addr", config.Addr,
			"db", config.DBPath,
			"stats", config.StatsEnabled,
			"stats_path", config.StatsPath,
			"stats_flush_interval", config.StatsFlushInterval.String(),
			"pairing_ttl", config.PairingTTL.String(),
			"config", config.ConfigLoadedFromPath,
			"version", version,
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutdown requested", "timeout", config.ShutdownTimeout.String())
	forceExit := time.AfterFunc(config.ShutdownTimeout, func() {
		logger.Error("shutdown timeout exceeded, forcing exit", "timeout", config.ShutdownTimeout.String())
		os.Exit(1)
	})
	defer forceExit.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	hub.Close()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
		_ = httpServer.Close()
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
