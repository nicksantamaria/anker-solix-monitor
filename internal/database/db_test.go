package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix"
)

const testAddr = "E8:EE:CC:7C:0A:2A"

func newTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func sampleStatus(ts time.Time) solix.DeviceStatus {
	return solix.DeviceStatus{
		BatteryPercent:          75,
		BatteryPercentExpansion: 50,
		BatteryHealth:           98,
		SolarPowerIn:            120,
		ACPowerIn:               10,
		ACPowerOut:              200,
		ACToBattery:             30,
		ACPowerOutSockets:       180,
		DC1PowerOut:             15,
		DC2PowerOut:             5,
		USBC1Power:              12,
		USBC2Power:              0,
		USBC3Power:              0,
		USBA1Power:              3,
		USBA2Power:              0,
		Temperature:             27,
		TimeRemainingHours:      4.5,
		SerialNumber:            "SN123456",
		SoftwareVersion:         "v1.2.3",
		UpdatedAt:               ts,
	}
}

func TestOpenAndSchema(t *testing.T) {
	db := newTestDB(t)
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// The telemetry table must exist.
	var name string
	err := db.sql.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='telemetry'`).Scan(&name)
	if err != nil {
		t.Fatalf("telemetry table missing: %v", err)
	}
	if name != "telemetry" {
		t.Fatalf("expected telemetry table, got %q", name)
	}
}

func TestInsertAndLatest(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	older := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	newer := time.Now().UTC().Truncate(time.Second)

	if err := db.Insert(ctx, testAddr, sampleStatus(older)); err != nil {
		t.Fatalf("Insert older: %v", err)
	}
	newStatus := sampleStatus(newer)
	newStatus.BatteryPercent = 42
	if err := db.Insert(ctx, testAddr, newStatus); err != nil {
		t.Fatalf("Insert newer: %v", err)
	}

	row, err := db.Latest(ctx, testAddr)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if row == nil {
		t.Fatal("Latest returned nil")
	}
	if row.BatteryPercent != 42 {
		t.Errorf("expected battery 42, got %d", row.BatteryPercent)
	}
	if row.SolarPowerW != 120 {
		t.Errorf("expected solar 120, got %d", row.SolarPowerW)
	}
	if row.SerialNumber != "SN123456" {
		t.Errorf("expected serial SN123456, got %q", row.SerialNumber)
	}
	if row.TimeRemainingHours != 4.5 {
		t.Errorf("expected time remaining 4.5, got %v", row.TimeRemainingHours)
	}
	if !row.Timestamp.Equal(newer) {
		t.Errorf("expected timestamp %v, got %v", newer, row.Timestamp)
	}
}

func TestHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		s := sampleStatus(base.Add(time.Duration(i) * time.Minute))
		s.BatteryPercent = 10 * i
		if err := db.Insert(ctx, testAddr, s); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}
	// Insert one for a different device to ensure filtering.
	if err := db.Insert(ctx, "AA:BB:CC:DD:EE:FF", sampleStatus(base)); err != nil {
		t.Fatalf("Insert other device: %v", err)
	}

	rows, err := db.History(ctx, testAddr, base.Add(-time.Minute), 100)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	// Ordered oldest-first.
	for i := 0; i < 5; i++ {
		if rows[i].BatteryPercent != 10*i {
			t.Errorf("row %d: expected battery %d, got %d", i, 10*i, rows[i].BatteryPercent)
		}
	}

	// Limit is respected.
	limited, err := db.History(ctx, testAddr, base.Add(-time.Minute), 2)
	if err != nil {
		t.Fatalf("History limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(limited))
	}

	// since filter excludes older rows.
	recent, err := db.History(ctx, testAddr, base.Add(3*time.Minute), 100)
	if err != nil {
		t.Fatalf("History recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent rows, got %d", len(recent))
	}
}

func TestEmptyDatabase(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	row, err := db.Latest(ctx, testAddr)
	if err != nil {
		t.Fatalf("Latest on empty: %v", err)
	}
	if row != nil {
		t.Fatalf("expected nil row, got %+v", row)
	}

	rows, err := db.History(ctx, testAddr, time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("History on empty: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}
