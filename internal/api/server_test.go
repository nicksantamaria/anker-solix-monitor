package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/internal/database"
)

type mockStore struct {
	latest    *database.TelemetryRow
	latestErr error
	history   []database.TelemetryRow
	histErr   error
	pingErr   error
}

func (m *mockStore) Latest(_ context.Context, _ string) (*database.TelemetryRow, error) {
	return m.latest, m.latestErr
}

func (m *mockStore) History(_ context.Context, _ string, _ time.Time, _ int) ([]database.TelemetryRow, error) {
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
	srv := newTestServer(&mockStore{latest: row}, &mockMonitor{connected: true})

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
	srv := newTestServer(&mockStore{history: rows}, &mockMonitor{})

	req := httptest.NewRequest(http.MethodGet, "/api/history?hours=6", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got []database.TelemetryRow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}

	// Empty history returns [] not null.
	srvEmpty := newTestServer(&mockStore{history: nil}, &mockMonitor{})
	recEmpty := httptest.NewRecorder()
	srvEmpty.Handler().ServeHTTP(recEmpty, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if recEmpty.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recEmpty.Code)
	}
	if body := recEmpty.Body.String(); body != "[]\n" {
		t.Errorf("expected empty array, got %q", body)
	}

	// Error case.
	srvErr := newTestServer(&mockStore{histErr: errors.New("boom")}, &mockMonitor{})
	recErr := httptest.NewRecorder()
	srvErr.Handler().ServeHTTP(recErr, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if recErr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", recErr.Code)
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

func TestAPIEndpointsSetNoCacheHeaders(t *testing.T) {
	srv := newTestServer(&mockStore{
		latest:  &database.TelemetryRow{ID: 1},
		history: []database.TelemetryRow{},
	}, &mockMonitor{})

	tests := []string{"/api/status", "/api/history", "/api/health"}
	for _, path := range tests {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
			t.Errorf("%s Cache-Control: got %q", path, got)
		}
		if got := rec.Header().Get("Pragma"); got != "no-cache" {
			t.Errorf("%s Pragma: got %q", path, got)
		}
		if got := rec.Header().Get("Expires"); got != "0" {
			t.Errorf("%s Expires: got %q", path, got)
		}
	}
}
