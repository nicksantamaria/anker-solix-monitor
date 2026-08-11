// Command solix-monitor is a self-contained monitoring service for an Anker
// Solix F2000 battery. It connects to the device over BLE, records telemetry
// to a local SQLite database, and serves a web dashboard.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/nicksantamaria/anker-solix-monitor/internal/api"
	"github.com/nicksantamaria/anker-solix-monitor/internal/config"
	"github.com/nicksantamaria/anker-solix-monitor/internal/database"
	"github.com/nicksantamaria/anker-solix-monitor/internal/monitor"
	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix"
)

func main() {
	cfg := config.Load()

	logger := setupLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	logger.Info("starting solix-monitor",
		"addr", cfg.BLEAddress,
		"db", cfg.DBPath,
		"listen", cfg.ListenAddr,
		"poll_interval", cfg.PollInterval,
	)

	db, err := database.Open(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("error closing database", "error", err)
		}
	}()

	client, err := solix.NewClient(solix.Config{
		ScanTimeout:    cfg.ScanTimeout,
		ConnectTimeout: cfg.ConnectTimeout,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("failed to initialise BLE client", "error", err)
		os.Exit(1)
	}

	mon := monitor.New(monitor.Config{
		BLEAddress:   cfg.BLEAddress,
		PollInterval: cfg.PollInterval,
	}, client, db)

	server := api.New(api.Config{
		ListenAddr: cfg.ListenAddr,
		DeviceAddr: cfg.BLEAddress,
	}, db, mon)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := mon.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("monitor stopped unexpectedly", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.Start(ctx); err != nil {
			logger.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received, stopping")

	wg.Wait()
	logger.Info("shutdown complete")
}

func setupLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
