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

	base := time.Now().UTC().Truncate(time.Minute)
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

func TestHistoryBucketed(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 120; i++ {
		s := sampleStatus(base.Add(time.Duration(i) * time.Second))
		s.BatteryPercent = i % 100
		if err := db.Insert(ctx, testAddr, s); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	rows, err := db.HistoryBucketed(ctx, testAddr, base.Add(-time.Minute), 60, 10)
	if err != nil {
		t.Fatalf("HistoryBucketed: %v", err)
	}
	if len(rows) < 2 || len(rows) > 3 {
		t.Fatalf("expected 2-3 bucketed rows, got %d", len(rows))
	}
	if rows[0].Timestamp.After(rows[1].Timestamp) {
		t.Fatalf("expected ascending timestamps, got %v then %v", rows[0].Timestamp, rows[1].Timestamp)
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

func TestApplyRetentionPolicy(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	db.nextRetentionRun = time.Now().Add(365 * 24 * time.Hour)

	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)

	insertRange := func(start time.Time, minutes int) {
		t.Helper()
		for i := 0; i < minutes; i++ {
			s := sampleStatus(start.Add(time.Duration(i) * time.Minute))
			s.BatteryPercent = i % 100
			if err := db.Insert(ctx, testAddr, s); err != nil {
				t.Fatalf("Insert at %v: %v", s.UpdatedAt, err)
			}
		}
	}

	// <24h raw window: should remain unbucketed.
	insertRange(now.Add(-2*time.Hour), 120)
	// 1-7d window: should compact to 5-minute buckets.
	insertRange(now.Add(-48*time.Hour), 360)
	// 7-30d window: should compact to 15-minute buckets.
	insertRange(now.Add(-10*24*time.Hour), 600)
	// >30d window: should be deleted.
	insertRange(now.Add(-40*24*time.Hour), 180)

	if err := db.applyRetention(ctx, now); err != nil {
		t.Fatalf("applyRetention: %v", err)
	}

	rawCount := countRowsInWindow(t, db, now.Add(-24*time.Hour), now)
	if rawCount != 120 {
		t.Fatalf("raw window count: got %d want %d", rawCount, 120)
	}

	fiveStart := now.Add(-7 * 24 * time.Hour)
	fiveEnd := now.Add(-24 * time.Hour)
	fiveCount := countRowsInWindow(t, db, fiveStart, fiveEnd)
	if fiveCount != 72 {
		t.Fatalf("5-minute window count: got %d want %d", fiveCount, 72)
	}
	assertBucketAligned(t, db, fiveStart, fiveEnd, 300)

	fifteenStart := now.Add(-30 * 24 * time.Hour)
	fifteenEnd := now.Add(-7 * 24 * time.Hour)
	fifteenCount := countRowsInWindow(t, db, fifteenStart, fifteenEnd)
	if fifteenCount != 40 {
		t.Fatalf("15-minute window count: got %d want %d", fifteenCount, 40)
	}
	assertBucketAligned(t, db, fifteenStart, fifteenEnd, 900)

	oldCount := countRowsBefore(t, db, now.Add(-30*24*time.Hour))
	if oldCount != 0 {
		t.Fatalf("expected no rows older than 30 days, got %d", oldCount)
	}
}

func countRowsInWindow(t *testing.T, db *DB, start, end time.Time) int {
	t.Helper()
	var count int
	err := db.sql.QueryRow(
		`SELECT COUNT(*) FROM telemetry WHERE device_addr = ? AND timestamp >= ? AND timestamp < ?`,
		testAddr,
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
	).Scan(&count)
	if err != nil {
		t.Fatalf("countRowsInWindow: %v", err)
	}
	return count
}

func countRowsBefore(t *testing.T, db *DB, before time.Time) int {
	t.Helper()
	var count int
	err := db.sql.QueryRow(
		`SELECT COUNT(*) FROM telemetry WHERE device_addr = ? AND timestamp < ?`,
		testAddr,
		before.Format(time.RFC3339),
	).Scan(&count)
	if err != nil {
		t.Fatalf("countRowsBefore: %v", err)
	}
	return count
}

func assertBucketAligned(t *testing.T, db *DB, start, end time.Time, bucketSeconds int) {
	t.Helper()
	var misaligned int
	err := db.sql.QueryRow(
		`SELECT COUNT(*) FROM telemetry
		 WHERE device_addr = ? AND timestamp >= ? AND timestamp < ?
		   AND (CAST(strftime('%s', timestamp) AS INTEGER) % ?) != 0`,
		testAddr,
		start.Format(time.RFC3339),
		end.Format(time.RFC3339),
		bucketSeconds,
	).Scan(&misaligned)
	if err != nil {
		t.Fatalf("assertBucketAligned: %v", err)
	}
	if misaligned != 0 {
		t.Fatalf("expected timestamps aligned to %d seconds, got %d misaligned rows", bucketSeconds, misaligned)
	}
}

func TestShouldVacuum(t *testing.T) {
	cases := []struct {
		name      string
		pageCount int64
		freePages int64
		want      bool
	}{
		{name: "zero pages", pageCount: 0, freePages: 2000, want: false},
		{name: "below free page threshold", pageCount: 10000, freePages: minVacuumFreePages - 1, want: false},
		{name: "below free ratio threshold", pageCount: 10000, freePages: 1500, want: false},
		{name: "meets both thresholds", pageCount: 5000, freePages: 1500, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldVacuum(tc.pageCount, tc.freePages)
			if got != tc.want {
				t.Fatalf("shouldVacuum(%d, %d) = %v, want %v", tc.pageCount, tc.freePages, got, tc.want)
			}
		})
	}
}
