package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/internal/database"
)

type mockStore struct {
	latest             *database.TelemetryRow
	latestErr          error
	history            []database.TelemetryRow
	histErr            error
	pingErr            error
	latestCalls        int
	historyCalls       int
	historyBucketCalls int
	lastBucketSeconds  int
	lastBucketLimit    int
}

func (m *mockStore) Latest(_ context.Context, _ string) (*database.TelemetryRow, error) {
	m.latestCalls++
	return m.latest, m.latestErr
}

func (m *mockStore) History(_ context.Context, _ string, _ time.Time, _ int) ([]database.TelemetryRow, error) {
	m.historyCalls++
	return m.history, m.histErr
}

func (m *mockStore) HistoryBucketed(_ context.Context, _ string, _ time.Time, bucketSeconds int, limit int) ([]database.TelemetryRow, error) {
	m.historyBucketCalls++
	m.lastBucketSeconds = bucketSeconds
	m.lastBucketLimit = limit
	return m.history, m.histErr
}

func (m *mockStore) Ping(_ context.Context) error { return m.pingErr }

type mockMonitor struct {
	lastPoll  time.Time
	lastErr   error
	connected bool
}

func (m *mockMonitor) LastPoll() time.Time { return m.lastPoll }
func (m *mockMonitor) LastError() error    { return m.lastErr }
func (m *mockMonitor) Connected() bool     { return m.connected }

func newTestServer(store Store, mon MonitorStatus) *Server {
	return New(Config{ListenAddr: "127.0.0.1:0", DeviceAddr: "E8:EE:CC:7C:0A:2A"}, store, mon)
}

func TestStatusEndpoint(t *testing.T) {
	row := &database.TelemetryRow{
		ID:             1,
		Timestamp:      time.Now().UTC(),
		DeviceAddr:     "E8:EE:CC:7C:0A:2A",
		BatteryPercent: 88,
		SolarPowerW:    150,
	}
	store := &mockStore{latest: row}
	srv := newTestServer(store, &mockMonitor{connected: true})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got database.TelemetryRow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.BatteryPercent != 88 {
		t.Errorf("expected battery 88, got %d", got.BatteryPercent)
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("expected cache control max-age=60, got %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("expected ETag header")
	}
	etag := rec.Header().Get("ETag")

	recCached := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recCached, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if store.latestCalls != 1 {
		t.Fatalf("expected latest store to be called once, got %d", store.latestCalls)
	}

	req304 := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req304.Header.Set("If-None-Match", etag)
	rec304 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec304, req304)
	if rec304.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rec304.Code)
	}

	// No data case.
	srv2 := newTestServer(&mockStore{latest: nil}, &mockMonitor{})
	rec2 := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for no data, got %d", rec2.Code)
	}
}

func TestHistoryEndpoint(t *testing.T) {
	rows := []database.TelemetryRow{
		{ID: 1, BatteryPercent: 10},
		{ID: 2, BatteryPercent: 20},
	}
	store := &mockStore{history: rows}
	srv := newTestServer(store, &mockMonitor{})

	req := httptest.NewRequest(http.MethodGet, "/api/history?hours=6", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if _, ok := got[0]["software_version"]; ok {
		t.Fatalf("did not expect software_version in history payload")
	}
	if _, ok := got[0]["serial_number"]; ok {
		t.Fatalf("did not expect serial_number in history payload")
	}
	if _, ok := got[0]["battery_percent"]; !ok {
		t.Fatalf("expected battery_percent in history payload")
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("expected cache control max-age=60, got %q", rec.Header().Get("Cache-Control"))
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header")
	}

	// Empty history returns [] not null.
	srvEmpty := newTestServer(&mockStore{history: nil}, &mockMonitor{})
	recEmpty := httptest.NewRecorder()
	srvEmpty.Handler().ServeHTTP(recEmpty, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if recEmpty.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recEmpty.Code)
	}
	if body := recEmpty.Body.String(); body != "[]" {
		t.Errorf("expected empty array, got %q", body)
	}

	// Error case.
	srvErr := newTestServer(&mockStore{histErr: errors.New("boom")}, &mockMonitor{})
	recErr := httptest.NewRecorder()
	srvErr.Handler().ServeHTTP(recErr, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if recErr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", recErr.Code)
	}

	// Repeat request should hit cache instead of store.
	recCached := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recCached, httptest.NewRequest(http.MethodGet, "/api/history?hours=6", nil))
	if store.historyBucketCalls != 1 {
		t.Fatalf("expected history bucket store to be called once, got %d", store.historyBucketCalls)
	}
	if store.historyCalls != 0 {
		t.Fatalf("expected non-bucket history store to not be called, got %d", store.historyCalls)
	}
	if store.lastBucketSeconds != 60 {
		t.Fatalf("expected 60s buckets for 6h history, got %d", store.lastBucketSeconds)
	}
	if store.lastBucketLimit != 360 {
		t.Fatalf("expected 360 point limit for 6h history, got %d", store.lastBucketLimit)
	}

	// Conditional request should return 304.
	req304 := httptest.NewRequest(http.MethodGet, "/api/history?hours=6", nil)
	req304.Header.Set("If-None-Match", etag)
	rec304 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec304, req304)
	if rec304.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rec304.Code)
	}
	if rec304.Body.Len() != 0 {
		t.Fatalf("expected empty 304 body, got %q", rec304.Body.String())
	}
}

func TestHistorySampling(t *testing.T) {
	cases := []struct {
		hours          int
		wantBucketSecs int
		wantPoints     int
	}{
		{hours: 1, wantBucketSecs: 30, wantPoints: 120},
		{hours: 6, wantBucketSecs: 60, wantPoints: 360},
		{hours: 24, wantBucketSecs: 60, wantPoints: 1440},
		{hours: 168, wantBucketSecs: 420, wantPoints: 1440},
	}

	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.hours), func(t *testing.T) {
			gotBucketSecs, gotPoints := historySampling(tc.hours)
			if gotBucketSecs != tc.wantBucketSecs {
				t.Fatalf("bucket seconds: got %d want %d", gotBucketSecs, tc.wantBucketSecs)
			}
			if gotPoints != tc.wantPoints {
				t.Fatalf("points: got %d want %d", gotPoints, tc.wantPoints)
			}
		})
	}
}

func TestHealthEndpoint(t *testing.T) {
	poll := time.Now().UTC()
	srv := newTestServer(&mockStore{}, &mockMonitor{
		lastPoll:  poll,
		connected: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("expected status ok, got %v", got["status"])
	}
	if got["ble_connected"] != true {
		t.Errorf("expected ble_connected true, got %v", got["ble_connected"])
	}
	if got["db_ok"] != true {
		t.Errorf("expected db_ok true, got %v", got["db_ok"])
	}
	if got["last_error"] != nil {
		t.Errorf("expected last_error nil, got %v", got["last_error"])
	}

	// Degraded DB with error.
	srv2 := newTestServer(&mockStore{pingErr: errors.New("db down")}, &mockMonitor{
		lastErr: errors.New("ble lost"),
	})
	rec2 := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var got2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &got2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got2["status"] != "degraded" {
		t.Errorf("expected degraded, got %v", got2["status"])
	}
	if got2["db_ok"] != false {
		t.Errorf("expected db_ok false, got %v", got2["db_ok"])
	}
	if got2["last_error"] != "ble lost" {
		t.Errorf("expected last_error 'ble lost', got %v", got2["last_error"])
	}
}

func TestIndexEndpoint(t *testing.T) {
	srv := newTestServer(&mockStore{}, &mockMonitor{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected content type: %q", ct)
	}
}
