// Package monitor implements the BLE polling loop that connects to a Solix
// device, records telemetry to a sink, and maintains connection health state.
package monitor

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix"
)

// TelemetrySink persists telemetry snapshots.
type TelemetrySink interface {
	Insert(ctx context.Context, deviceAddr string, s solix.DeviceStatus) error
}

// BLEClient abstracts the subset of *solix.Client used by the monitor so it
// can be mocked in tests.
type BLEClient interface {
	Connect(ctx context.Context, addr string) (BLEDevice, error)
}

// BLEDevice abstracts the subset of *solix.Device used by the monitor.
type BLEDevice interface {
	Refresh(ctx context.Context) error
	Status(ctx context.Context) (solix.DeviceStatus, error)
	AddCallback(fn solix.StateChangeCallback)
	Disconnected() <-chan struct{}
	Disconnect()
}

// Config configures a Monitor.
type Config struct {
	BLEAddress   string
	PollInterval time.Duration
}

// Monitor manages the lifecycle of a BLE connection and telemetry collection.
type Monitor struct {
	cfg    Config
	client BLEClient
	sink   TelemetrySink
	log    *slog.Logger

	mu        sync.RWMutex
	lastPoll  time.Time
	lastErr   error
	connected bool
}

// solixClientAdapter adapts *solix.Client to the BLEClient interface.
type solixClientAdapter struct {
	c *solix.Client
}

func (a solixClientAdapter) Connect(ctx context.Context, addr string) (BLEDevice, error) {
	dev, err := a.c.Connect(ctx, addr)
	if err != nil {
		return nil, err
	}
	return dev, nil
}

// New creates a Monitor backed by a real *solix.Client.
func New(cfg Config, client *solix.Client, sink TelemetrySink) *Monitor {
	return NewWithClient(cfg, solixClientAdapter{c: client}, sink, slog.Default())
}

// NewWithClient creates a Monitor with an injected BLEClient, primarily for
// testing.
func NewWithClient(cfg Config, client BLEClient, sink TelemetrySink, log *slog.Logger) *Monitor {
	if log == nil {
		log = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	return &Monitor{
		cfg:    cfg,
		client: client,
		sink:   sink,
		log:    log,
	}
}

// LastPoll returns the time of the last successful telemetry read.
func (m *Monitor) LastPoll() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastPoll
}

// LastError returns the most recent error encountered (nil if healthy).
func (m *Monitor) LastError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastErr
}

// Connected reports whether the monitor currently holds a BLE connection.
func (m *Monitor) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *Monitor) setConnected(v bool) {
	m.mu.Lock()
	m.connected = v
	m.mu.Unlock()
}

func (m *Monitor) setError(err error) {
	m.mu.Lock()
	m.lastErr = err
	m.mu.Unlock()
}

func (m *Monitor) recordPoll() {
	m.mu.Lock()
	m.lastPoll = time.Now()
	m.lastErr = nil
	m.mu.Unlock()
}

const (
	minBackoff = 5 * time.Second
	maxBackoff = 60 * time.Second
)

// Run drives the connect/collect/reconnect loop until ctx is cancelled. It
// only returns an error if ctx is done (returning ctx.Err()).
func (m *Monitor) Run(ctx context.Context) error {
	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := m.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			m.setConnected(false)
			m.setError(err)
			m.log.Warn("session ended, backing off", "error", err, "backoff", backoff)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Clean disconnect (not an error); reset backoff and retry.
		backoff = minBackoff
	}
}

// session establishes one connection and collects telemetry until disconnect,
// error, or context cancellation.
func (m *Monitor) session(ctx context.Context) error {
	connectCtx := ctx
	m.log.Info("connecting to device", "addr", m.cfg.BLEAddress)

	dev, err := m.client.Connect(connectCtx, m.cfg.BLEAddress)
	if err != nil {
		return err
	}
	defer dev.Disconnect()

	m.setConnected(true)
	m.log.Info("connected", "addr", m.cfg.BLEAddress)

	// Store telemetry on every push update.
	dev.AddCallback(func(status solix.DeviceStatus) {
		if err := m.sink.Insert(context.Background(), m.cfg.BLEAddress, status); err != nil {
			m.log.Error("insert telemetry (callback)", "error", err)
			return
		}
		m.log.Info("telemetry received via push", "battery_pct", status.BatteryPercent, "updated_at", status.UpdatedAt)
		m.recordPoll()
	})

	// Poll immediately, then on interval.
	m.poll(ctx, dev)

	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-dev.Disconnected():
			m.setConnected(false)
			return errors.New("device disconnected")
		case <-ticker.C:
			m.poll(ctx, dev)
		}
	}
}

// poll requests fresh telemetry from the device and stores any result that
// arrives. ErrNoData is not treated as an error condition (telemetry may
// simply not have arrived yet).
func (m *Monitor) poll(ctx context.Context, dev BLEDevice) {
	// Ask the device for a fresh snapshot. For push-only devices this is a
	// no-op; for query-response devices (e.g. F2000) it sends the query frame
	// and the response arrives asynchronously via the registered callback.
	if err := dev.Refresh(ctx); err != nil {
		m.log.Warn("poll refresh request failed", "error", err)
		m.setError(err)
		return
	}

	status, err := dev.Status(ctx)
	if err != nil {
		if errors.Is(err, solix.ErrNoData) {
			m.log.Debug("no telemetry data yet")
			return
		}
		m.log.Error("poll status", "error", err)
		m.setError(err)
		return
	}
	if err := m.sink.Insert(ctx, m.cfg.BLEAddress, status); err != nil {
		m.log.Error("insert telemetry (poll)", "error", err)
		m.setError(err)
		return
	}
	m.log.Info("poll successful", "battery_pct", status.BatteryPercent, "updated_at", status.UpdatedAt)
	m.recordPoll()
}
