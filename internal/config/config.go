// Package config loads configuration for the solix-monitor service from
// command-line flags and environment variables via urfave/cli/v3.
package config

import (
	"time"

	"github.com/urfave/cli/v3"
)

// Config holds all runtime configuration for the monitoring service.
type Config struct {
	BLEAddress     string
	DBPath         string
	ListenAddr     string
	PollInterval   time.Duration
	LogLevel       string
	ConnectTimeout time.Duration
	ScanTimeout    time.Duration
}

// Default values used when neither an environment variable nor a flag is set.
const (
	defaultDBPath         = "./solix.db"
	defaultListenAddr     = "0.0.0.0:8080"
	defaultPollInterval   = 30 * time.Second
	defaultLogLevel       = "info"
	defaultConnectTimeout = 30 * time.Second
	defaultScanTimeout    = 10 * time.Second
)

// Flags returns the cli.Flag slice to be registered on the solix-monitor command.
func Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "addr",
			Usage:   "BLE address of the device (MAC address or UUID; required)",
			Sources: cli.EnvVars("SOLIX_ADDRESS"),
		},
		&cli.StringFlag{
			Name:    "db",
			Value:   defaultDBPath,
			Usage:   "SQLite database file path",
			Sources: cli.EnvVars("SOLIX_DB_PATH"),
		},
		&cli.StringFlag{
			Name:    "listen",
			Value:   defaultListenAddr,
			Usage:   "HTTP server bind address",
			Sources: cli.EnvVars("SOLIX_LISTEN"),
		},
		&cli.DurationFlag{
			Name:    "poll-interval",
			Value:   defaultPollInterval,
			Usage:   "telemetry polling interval",
			Sources: cli.EnvVars("SOLIX_POLL_INTERVAL"),
		},
		&cli.StringFlag{
			Name:    "log-level",
			Value:   defaultLogLevel,
			Usage:   "log level (debug, info, warn, error)",
			Sources: cli.EnvVars("SOLIX_LOG_LEVEL"),
		},
		&cli.DurationFlag{
			Name:    "connect-timeout",
			Value:   defaultConnectTimeout,
			Usage:   "BLE connect timeout",
			Sources: cli.EnvVars("SOLIX_CONNECT_TIMEOUT"),
		},
		&cli.DurationFlag{
			Name:    "scan-timeout",
			Value:   defaultScanTimeout,
			Usage:   "BLE scan timeout",
			Sources: cli.EnvVars("SOLIX_SCAN_TIMEOUT"),
		},
	}
}

// FromCLI builds a Config from a parsed cli.Command.
func FromCLI(cmd *cli.Command) Config {
	return Config{
		BLEAddress:     cmd.String("addr"),
		DBPath:         cmd.String("db"),
		ListenAddr:     cmd.String("listen"),
		PollInterval:   cmd.Duration("poll-interval"),
		LogLevel:       cmd.String("log-level"),
		ConnectTimeout: cmd.Duration("connect-timeout"),
		ScanTimeout:    cmd.Duration("scan-timeout"),
	}
}
