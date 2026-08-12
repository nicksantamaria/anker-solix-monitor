// Package api implements the HTTP server exposing telemetry data and the
// embedded dashboard for the solix-monitor service.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/internal/database"
)

// Store provides read access to telemetry.
type Store interface {
	Latest(ctx context.Context, deviceAddr string) (*database.TelemetryRow, error)
	History(ctx context.Context, deviceAddr string, since time.Time, limit int) ([]database.TelemetryRow, error)
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
}

// New creates a Server.
func New(cfg Config, store Store, mon MonitorStatus) *Server {
	s := &Server{
		cfg:       cfg,
		store:     store,
		mon:       mon,
		log:       slog.Default(),
		startedAt: time.Now(),
	}
	s.handler = s.routes()
	return s
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

func setNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
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
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	if hours > 168 {
		hours = 168
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	rows, err := s.store.History(r.Context(), s.cfg.DeviceAddr, since, 10000)
	if err != nil {
		s.log.Error("history", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load history"})
		return
	}
	if rows == nil {
		rows = []database.TelemetryRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
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
