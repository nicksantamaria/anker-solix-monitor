// Package models defines the typed device models for Anker Solix power stations.
package models

// DeviceModel identifies the specific Solix device variant.
type DeviceModel string

const (
	// ModelF2000 is the Anker Solix F2000(P) / 767 PowerHouse (A1780).
	ModelF2000 DeviceModel = "F2000"
	// ModelUnknown is used when the device model cannot be determined.
	ModelUnknown DeviceModel = "Unknown"
)

// Device is the common interface implemented by all Solix device models.
type Device interface {
	// Model returns the device model identifier.
	Model() DeviceModel
	// ParseTelemetry parses a raw parameter map (from the protocol decoder)
	// into the device-specific status struct.
	ParseTelemetry(params map[string][]byte) error
	// ExpectedTelemetryLength returns the expected total payload length for
	// completeness checks. 0 means unconstrained.
	ExpectedTelemetryLength() int
}
