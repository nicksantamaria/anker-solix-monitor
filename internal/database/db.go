// Package database provides SQLite-backed persistence for Solix telemetry
// using the pure-Go modernc.org/sqlite driver (no CGO required).
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix"

	_ "modernc.org/sqlite"
)

// schema is the DDL applied on Open. It is idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS telemetry (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp               DATETIME NOT NULL,
    device_addr             TEXT NOT NULL,
    battery_percent         INTEGER,
    battery_percent_exp     INTEGER,
    battery_health          INTEGER,
    solar_power_w           INTEGER,
    ac_power_in_w           INTEGER,
    ac_power_out_w          INTEGER,
    ac_to_battery_w         INTEGER,
    ac_out_sockets_w        INTEGER,
    dc1_power_out_w         INTEGER,
    dc2_power_out_w         INTEGER,
    usbc1_power_w           INTEGER,
    usbc2_power_w           INTEGER,
    usbc3_power_w           INTEGER,
    usba1_power_w           INTEGER,
    usba2_power_w           INTEGER,
    temperature_c           INTEGER,
    time_remaining_hours    REAL,
    serial_number           TEXT,
    software_version        TEXT
);

CREATE INDEX IF NOT EXISTS idx_telemetry_timestamp ON telemetry(timestamp);
CREATE INDEX IF NOT EXISTS idx_telemetry_device    ON telemetry(device_addr);
`

// TelemetryRow mirrors a row in the telemetry table.
type TelemetryRow struct {
	ID                 int64     `json:"id"`
	Timestamp          time.Time `json:"timestamp"`
	DeviceAddr         string    `json:"device_addr"`
	BatteryPercent     int       `json:"battery_percent"`
	BatteryPercentExp  int       `json:"battery_percent_exp"`
	BatteryHealth      int       `json:"battery_health"`
	SolarPowerW        int       `json:"solar_power_w"`
	ACPowerInW         int       `json:"ac_power_in_w"`
	ACPowerOutW        int       `json:"ac_power_out_w"`
	ACToBatteryW       int       `json:"ac_to_battery_w"`
	ACOutSocketsW      int       `json:"ac_out_sockets_w"`
	DC1PowerOutW       int       `json:"dc1_power_out_w"`
	DC2PowerOutW       int       `json:"dc2_power_out_w"`
	USBC1PowerW        int       `json:"usbc1_power_w"`
	USBC2PowerW        int       `json:"usbc2_power_w"`
	USBC3PowerW        int       `json:"usbc3_power_w"`
	USBA1PowerW        int       `json:"usba1_power_w"`
	USBA2PowerW        int       `json:"usba2_power_w"`
	TemperatureC       int       `json:"temperature_c"`
	TimeRemainingHours float64   `json:"time_remaining_hours"`
	SerialNumber       string    `json:"serial_number"`
	SoftwareVersion    string    `json:"software_version"`
}

// DB wraps a *sql.DB connection to the SQLite telemetry store.
type DB struct {
	sql *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and applies
// the schema.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("database: open %q: %w", path, err)
	}

	// SQLite handles a single writer best with one connection; keep it modest.
	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.Exec(schema); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("database: apply schema: %w", err)
	}

	return &DB{sql: sqlDB}, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.sql.Close()
}

// Ping verifies connectivity to the database.
func (db *DB) Ping(ctx context.Context) error {
	return db.sql.PingContext(ctx)
}

// Insert stores a telemetry snapshot for the given device address. The
// timestamp is taken from s.UpdatedAt (falling back to now) and stored as UTC.
func (db *DB) Insert(ctx context.Context, deviceAddr string, s solix.DeviceStatus) error {
	ts := s.UpdatedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	ts = ts.UTC()

	const q = `
INSERT INTO telemetry (
    timestamp, device_addr, battery_percent, battery_percent_exp, battery_health,
    solar_power_w, ac_power_in_w, ac_power_out_w, ac_to_battery_w, ac_out_sockets_w,
    dc1_power_out_w, dc2_power_out_w, usbc1_power_w, usbc2_power_w, usbc3_power_w,
    usba1_power_w, usba2_power_w, temperature_c, time_remaining_hours,
    serial_number, software_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := db.sql.ExecContext(ctx, q,
		ts.Format(time.RFC3339),
		deviceAddr,
		s.BatteryPercent,
		s.BatteryPercentExpansion,
		s.BatteryHealth,
		s.SolarPowerIn,
		s.ACPowerIn,
		s.ACPowerOut,
		s.ACToBattery,
		s.ACPowerOutSockets,
		s.DC1PowerOut,
		s.DC2PowerOut,
		s.USBC1Power,
		s.USBC2Power,
		s.USBC3Power,
		s.USBA1Power,
		s.USBA2Power,
		s.Temperature,
		s.TimeRemainingHours,
		s.SerialNumber,
		s.SoftwareVersion,
	)
	if err != nil {
		return fmt.Errorf("database: insert telemetry: %w", err)
	}
	return nil
}

const selectColumns = `
    id, timestamp, device_addr, battery_percent, battery_percent_exp, battery_health,
    solar_power_w, ac_power_in_w, ac_power_out_w, ac_to_battery_w, ac_out_sockets_w,
    dc1_power_out_w, dc2_power_out_w, usbc1_power_w, usbc2_power_w, usbc3_power_w,
    usba1_power_w, usba2_power_w, temperature_c, time_remaining_hours,
    serial_number, software_version`

// scanRow scans a single telemetry row from a *sql.Row or *sql.Rows.
func scanRow(sc interface {
	Scan(dest ...any) error
}) (TelemetryRow, error) {
	var r TelemetryRow
	var tsStr string
	err := sc.Scan(
		&r.ID, &tsStr, &r.DeviceAddr, &r.BatteryPercent, &r.BatteryPercentExp, &r.BatteryHealth,
		&r.SolarPowerW, &r.ACPowerInW, &r.ACPowerOutW, &r.ACToBatteryW, &r.ACOutSocketsW,
		&r.DC1PowerOutW, &r.DC2PowerOutW, &r.USBC1PowerW, &r.USBC2PowerW, &r.USBC3PowerW,
		&r.USBA1PowerW, &r.USBA2PowerW, &r.TemperatureC, &r.TimeRemainingHours,
		&r.SerialNumber, &r.SoftwareVersion,
	)
	if err != nil {
		return TelemetryRow{}, err
	}
	r.Timestamp = parseTimestamp(tsStr)
	return r, nil
}

// parseTimestamp parses timestamps stored by Insert (RFC3339) as well as the
// default SQLite datetime format, returning UTC.
func parseTimestamp(s string) time.Time {
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// Latest returns the most recent telemetry row for the given device, or
// (nil, nil) if none exists.
func (db *DB) Latest(ctx context.Context, deviceAddr string) (*TelemetryRow, error) {
	q := `SELECT` + selectColumns + `
FROM telemetry WHERE device_addr = ? ORDER BY timestamp DESC, id DESC LIMIT 1`

	row := db.sql.QueryRowContext(ctx, q, deviceAddr)
	r, err := scanRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("database: latest: %w", err)
	}
	return &r, nil
}

// History returns telemetry rows for the device since the given time (UTC),
// ordered oldest-first, up to limit rows.
func (db *DB) History(ctx context.Context, deviceAddr string, since time.Time, limit int) ([]TelemetryRow, error) {
	if limit <= 0 {
		limit = 1000
	}
	q := `SELECT` + selectColumns + `
FROM telemetry WHERE device_addr = ? AND timestamp >= ? ORDER BY timestamp ASC, id ASC LIMIT ?`

	rows, err := db.sql.QueryContext(ctx, q, deviceAddr, since.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("database: history: %w", err)
	}
	defer rows.Close()

	var out []TelemetryRow
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("database: history scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: history rows: %w", err)
	}
	return out, nil
}
