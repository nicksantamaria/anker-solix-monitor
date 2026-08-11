// Package config loads configuration for the solix-monitor service from
// environment variables with command-line flag fallbacks.
package config

import (
	"flag"
	"os"
	"time"
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

// Load builds a Config from environment variables, falling back to
// command-line flags and then to built-in defaults. Environment variables
// take precedence over flags.
func Load() Config {
	fs := flag.NewFlagSet("solix-monitor", flag.ContinueOnError)

	addr := fs.String("addr", "", "BLE address of the device (MAC address or UUID; required)")
	db := fs.String("db", defaultDBPath, "SQLite database file path")
	listen := fs.String("listen", defaultListenAddr, "HTTP server bind address")
	poll := fs.Duration("poll-interval", defaultPollInterval, "telemetry polling interval")
	logLevel := fs.String("log-level", defaultLogLevel, "log level (debug, info, warn, error)")
	connectTimeout := fs.Duration("connect-timeout", defaultConnectTimeout, "BLE connect timeout")
	scanTimeout := fs.Duration("scan-timeout", defaultScanTimeout, "BLE scan timeout")

	// Ignore parse errors (e.g. during tests); defaults remain in place.
	_ = fs.Parse(os.Args[1:])

	cfg := Config{
		BLEAddress:     *addr,
		DBPath:         *db,
		ListenAddr:     *listen,
		PollInterval:   *poll,
		LogLevel:       *logLevel,
		ConnectTimeout: *connectTimeout,
		ScanTimeout:    *scanTimeout,
	}

	if v := os.Getenv("SOLIX_ADDRESS"); v != "" {
		cfg.BLEAddress = v
	}
	if v := os.Getenv("SOLIX_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("SOLIX_LISTEN"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("SOLIX_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PollInterval = d
		}
	}
	if v := os.Getenv("SOLIX_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("SOLIX_CONNECT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ConnectTimeout = d
		}
	}
	if v := os.Getenv("SOLIX_SCAN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ScanTimeout = d
		}
	}

	return cfg
}
