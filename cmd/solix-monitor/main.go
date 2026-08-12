// Command solix-monitor is a self-contained monitoring service for an Anker
// Solix F2000 battery. It connects to the device over BLE, records telemetry
// to a local SQLite database, and serves a web dashboard.
package main

import (
	"context"
	"fmt"
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
	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:  "solix-monitor",
		Usage: "Monitoring service for an Anker Solix F2000 battery",
		Flags: config.Flags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg := config.FromCLI(cmd)

			logger := setupLogger(cfg.LogLevel)
			slog.SetDefault(logger)

			if cfg.BLEAddress == "" {
				return fmt.Errorf("BLE device address is required: use --addr flag or SOLIX_ADDRESS environment variable")
			}
			logger.Info("starting solix-monitor",
				"addr", cfg.BLEAddress,
				"db", cfg.DBPath,
				"listen", cfg.ListenAddr,
				"poll_interval", cfg.PollInterval,
			)

			db, err := database.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
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
				return fmt.Errorf("failed to initialise BLE client: %w", err)
			}

			mon := monitor.New(monitor.Config{
				BLEAddress:   cfg.BLEAddress,
				PollInterval: cfg.PollInterval,
			}, client, db)

			server := api.New(api.Config{
				ListenAddr: cfg.ListenAddr,
				DeviceAddr: cfg.BLEAddress,
			}, db, mon)

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
				}
			}()

			<-ctx.Done()
			logger.Info("shutdown signal received, stopping")

			wg.Wait()
			logger.Info("shutdown complete")
			return nil
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
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
