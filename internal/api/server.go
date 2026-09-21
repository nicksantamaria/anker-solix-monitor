// Package api implements the HTTP server exposing telemetry data and the
// embedded dashboard for the solix-monitor service.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/internal/database"
)

// Store provides read access to telemetry.
type Store interface {
	Latest(ctx context.Context, deviceAddr string) (*database.TelemetryRow, error)
	History(ctx context.Context, deviceAddr string, since time.Time, limit int) ([]database.TelemetryRow, error)
	HistoryBucketed(ctx context.Context, deviceAddr string, since time.Time, bucketSeconds int, limit int) ([]database.TelemetryRow, error)
	Ping(ctx context.Context) error
}

// MonitorStatus exposes BLE connection health.
type MonitorStatus interface {
	LastPoll() time.Time
	LastError() error
	Connected() bool
}

// Config configures a Server.
type Config struct {
	ListenAddr string
	DeviceAddr string
}

// Server is the HTTP server for the monitoring dashboard and API.
type Server struct {
	cfg       Config
	store     Store
	mon       MonitorStatus
	log       *slog.Logger
	startedAt time.Time
	handler   http.Handler
	cacheTTL  time.Duration
	statusC   responseCache
	historyC  historyCache
}

// New creates a Server.
func New(cfg Config, store Store, mon MonitorStatus) *Server {
	s := &Server{
		cfg:       cfg,
		store:     store,
		mon:       mon,
		log:       slog.Default(),
		startedAt: time.Now(),
		cacheTTL:  time.Minute,
	}
	s.handler = s.routes()
	return s
}

type responseCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	payload   []byte
	etag      string
}

func (c *responseCache) get(now time.Time) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.payload) == 0 || now.After(c.expiresAt) {
		return nil, "", false
	}
	return c.payload, c.etag, true
}

func (c *responseCache) set(now time.Time, ttl time.Duration, payload []byte, etag string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.payload = payload
	c.etag = etag
	c.expiresAt = now.Add(ttl)
}

type historyCache struct {
	mu    sync.Mutex
	items map[int]*responseCache
}

func (c *historyCache) forHours(hours int) *responseCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[int]*responseCache{}
	}
	if item, ok := c.items[hours]; ok {
		return item
	}
	item := &responseCache{}
	c.items[hours] = item
	return item
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/", s.handleIndex)
	return mux
}

// Handler exposes the router for testing.
func (s *Server) Handler() http.Handler { return s.handler }

// Start runs the HTTP server until ctx is cancelled, then gracefully shuts
// down.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeCachedJSON(w http.ResponseWriter, r *http.Request, payload []byte, etag string, maxAge time.Duration) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(maxAge/time.Second)))
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func etagFor(payload []byte) string {
	sum := sha256.Sum256(payload)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if payload, etag, ok := s.statusC.get(now); ok {
		writeCachedJSON(w, r, payload, etag, s.cacheTTL)
		return
	}

	row, err := s.store.Latest(r.Context(), s.cfg.DeviceAddr)
	if err != nil {
		s.log.Error("status: latest", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load status"})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no telemetry available"})
		return
	}

	type statusResponse struct {
		ID                 int64     `json:"id"`
		Timestamp          time.Time `json:"timestamp"`
		DeviceAddr         string    `json:"device_addr"`
		BatteryPercent     int       `json:"battery_percent"`
		BatteryPercentExp  int       `json:"battery_percent_exp"`
		SolarPowerW        int       `json:"solar_power_w"`
		ACPowerInW         int       `json:"ac_power_in_w"`
		ACPowerOutW        int       `json:"ac_power_out_w"`
		ACToBatteryW       int       `json:"ac_to_battery_w"`
		ACOutSocketsW      int       `json:"ac_out_sockets_w"`
		DC1PowerOutW       int       `json:"dc1_power_out_w"`
		DC2PowerOutW       int       `json:"dc2_power_out_w"`
		TemperatureC       int       `json:"temperature_c"`
		TimeRemainingHours float64   `json:"time_remaining_hours"`
		SerialNumber       string    `json:"serial_number"`
		SoftwareVersion    string    `json:"software_version"`
	}

	payload, err := json.Marshal(statusResponse{
		ID:                 row.ID,
		Timestamp:          row.Timestamp,
		DeviceAddr:         row.DeviceAddr,
		BatteryPercent:     row.BatteryPercent,
		BatteryPercentExp:  row.BatteryPercentExp,
		SolarPowerW:        row.SolarPowerW,
		ACPowerInW:         row.ACPowerInW,
		ACPowerOutW:        row.ACPowerOutW,
		ACToBatteryW:       row.ACToBatteryW,
		ACOutSocketsW:      row.ACOutSocketsW,
		DC1PowerOutW:       row.DC1PowerOutW,
		DC2PowerOutW:       row.DC2PowerOutW,
		TemperatureC:       row.TemperatureC,
		TimeRemainingHours: row.TimeRemainingHours,
		SerialNumber:       row.SerialNumber,
		SoftwareVersion:    row.SoftwareVersion,
	})
	if err != nil {
		s.log.Error("status: marshal", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode status"})
		return
	}
	etag := etagFor(payload)
	s.statusC.set(now, s.cacheTTL, payload, etag)
	writeCachedJSON(w, r, payload, etag, s.cacheTTL)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	if hours > 168 {
		hours = 168
	}

	now := time.Now()
	cache := s.historyC.forHours(hours)
	if payload, etag, ok := cache.get(now); ok {
		writeCachedJSON(w, r, payload, etag, s.cacheTTL)
		return
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	bucketSeconds, pointLimit := historySampling(hours)
	rows, err := s.store.HistoryBucketed(r.Context(), s.cfg.DeviceAddr, since, bucketSeconds, pointLimit)
	if err != nil {
		s.log.Error("history", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load history"})
		return
	}

	type historyPoint struct {
		Timestamp      time.Time `json:"timestamp"`
		BatteryPercent int       `json:"battery_percent"`
		SolarPowerW    int       `json:"solar_power_w"`
		ACPowerInW     int       `json:"ac_power_in_w"`
		ACPowerOutW    int       `json:"ac_power_out_w"`
		DC1PowerOutW   int       `json:"dc1_power_out_w"`
		DC2PowerOutW   int       `json:"dc2_power_out_w"`
		TemperatureC   int       `json:"temperature_c"`
	}
	points := make([]historyPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, historyPoint{
			Timestamp:      row.Timestamp,
			BatteryPercent: row.BatteryPercent,
			SolarPowerW:    row.SolarPowerW,
			ACPowerInW:     row.ACPowerInW,
			ACPowerOutW:    row.ACPowerOutW,
			DC1PowerOutW:   row.DC1PowerOutW,
			DC2PowerOutW:   row.DC2PowerOutW,
			TemperatureC:   row.TemperatureC,
		})
	}

	payload, err := json.Marshal(points)
	if err != nil {
		s.log.Error("history: marshal", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode history"})
		return
	}

	etag := etagFor(payload)
	cache.set(now, s.cacheTTL, payload, etag)
	writeCachedJSON(w, r, payload, etag, s.cacheTTL)
}

func historySampling(hours int) (bucketSeconds int, pointLimit int) {
	const (
		minPoints = 120
		maxPoints = 1440
	)

	points := hours * 60
	if points < minPoints {
		points = minPoints
	}
	if points > maxPoints {
		points = maxPoints
	}

	windowSeconds := hours * 60 * 60
	bucket := windowSeconds / points
	if windowSeconds%points != 0 {
		bucket++
	}
	if bucket < 1 {
		bucket = 1
	}

	return bucket, points
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dbOK := s.store.Ping(r.Context()) == nil

	var lastErr *string
	if e := s.mon.LastError(); e != nil {
		msg := e.Error()
		lastErr = &msg
	}

	var lastPoll *time.Time
	if p := s.mon.LastPoll(); !p.IsZero() {
		lastPoll = &p
	}

	status := "ok"
	if !dbOK {
		status = "degraded"
	}

	resp := map[string]any{
		"status":         status,
		"uptime_seconds": int(time.Since(s.startedAt).Seconds()),
		"ble_connected":  s.mon.Connected(),
		"last_poll":      lastPoll,
		"last_error":     lastErr,
		"db_ok":          dbOK,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(webFS, "web/index.html")
	if err != nil {
		http.Error(w, "dashboard unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
