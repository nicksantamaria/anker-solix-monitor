package solix

import (
	"context"
	"sync"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix/models"
)

// DiscoveredDevice holds information about a Solix BLE device found during scanning.
type DiscoveredDevice struct {
	// Addr is the Bluetooth MAC address (e.g. "E8:EE:CC:7C:0A:2A").
	Addr string
	// Name is the Bluetooth advertised device name (e.g. "767_PowerHouse").
	Name string
	// RSSI is the received signal strength in dBm.
	RSSI int
	// SeenAt records when the advertisement was received.
	SeenAt time.Time
}

// DeviceStatus is the unified telemetry snapshot returned by Device.Status.
// Fields that are not available for a particular model will be -1.
type DeviceStatus struct {
	// Model is the identified device model.
	Model models.DeviceModel

	// Battery
	BatteryPercent          int
	BatteryPercentExpansion int
	BatteryHealth           int
	BatteryHealthExpansion  int
	NumExpansion            int

	// Time remaining to full/empty
	TimeRemainingHours float64
	DaysRemaining      int
	HoursRemaining     float64
	// TimestampRemaining is nil when TimeRemainingHours is unavailable.
	TimestampRemaining *time.Time

	// Power (Watts)
	SolarPowerIn      int
	ACPowerIn         int
	ACPowerOut        int
	ACToBattery       int
	ACPowerOutSockets int
	DC1PowerOut       int
	DC2PowerOut       int
	USBC1Power        int
	USBC2Power        int
	USBC3Power        int
	USBA1Power        int
	USBA2Power        int

	// Temperature (°C, -1 = unknown)
	Temperature          int
	TemperatureExpansion int

	// Firmware
	SoftwareVersion           string
	SoftwareVersionExpansion  string
	SoftwareVersionController string

	// Serial number
	SerialNumber string

	// UpdatedAt is when this snapshot was populated.
	UpdatedAt time.Time
}

// StateChangeCallback is called whenever the device reports new telemetry.
type StateChangeCallback func(status DeviceStatus)

// Device represents an active connection to a Solix power station.
// All methods are safe for concurrent use.
type Device struct {
	mu          sync.RWMutex
	addr        string
	name        string
	model       models.DeviceModel
	status      *DeviceStatus
	callbacks   []StateChangeCallback
	cancelConn  context.CancelFunc
	disconnected chan struct{}
}

func newDevice(addr, name string, model models.DeviceModel) *Device {
	return &Device{
		addr:         addr,
		name:         name,
		model:        model,
		disconnected: make(chan struct{}),
	}
}

// Addr returns the Bluetooth MAC address.
func (d *Device) Addr() string { return d.addr }

// Name returns the Bluetooth advertised name.
func (d *Device) Name() string { return d.name }

// Model returns the identified device model.
func (d *Device) Model() models.DeviceModel { return d.model }

// AddCallback registers a function to be called on every telemetry update.
func (d *Device) AddCallback(fn StateChangeCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks = append(d.callbacks, fn)
}

// RemoveCallback deregisters a previously registered callback.
func (d *Device) RemoveCallback(fn StateChangeCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	newCBs := d.callbacks[:0]
	for _, cb := range d.callbacks {
		// Compare function pointers via reflection is not idiomatic; we use
		// a simple linear scan removing the first match.
		if &cb != &fn {
			newCBs = append(newCBs, cb)
		}
	}
	d.callbacks = newCBs
}

// Status returns the most recently received telemetry snapshot.
// Returns ErrNoData if no telemetry has been received yet.
func (d *Device) Status(_ context.Context) (DeviceStatus, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.status == nil {
		return DeviceStatus{}, ErrNoData
	}
	return *d.status, nil
}

// Disconnected returns a channel that is closed when the device disconnects.
func (d *Device) Disconnected() <-chan struct{} {
	return d.disconnected
}

// Disconnect closes the BLE connection.
func (d *Device) Disconnect() {
	d.mu.Lock()
	cancel := d.cancelConn
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *Device) updateStatus(s DeviceStatus) {
	d.mu.Lock()
	d.status = &s
	cbs := make([]StateChangeCallback, len(d.callbacks))
	copy(cbs, d.callbacks)
	d.mu.Unlock()

	for _, cb := range cbs {
		cb(s)
	}
}

func (d *Device) markDisconnected() {
	select {
	case <-d.disconnected:
		// already closed
	default:
		close(d.disconnected)
	}
}
