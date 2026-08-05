package solix

import "errors"

// ErrNotConnected is returned when an operation requires an active BLE
// connection but none exists.
var ErrNotConnected = errors.New("solix: not connected to device")

// ErrNotNegotiated is returned when an operation requires a completed
// encryption negotiation but it has not yet been performed.
var ErrNotNegotiated = errors.New("solix: encryption negotiation not completed")

// ErrNoData is returned when telemetry data has not yet been received.
var ErrNoData = errors.New("solix: no telemetry data available yet")

// ErrUnsupportedDevice is returned when the device does not match any known
// Solix model.
var ErrUnsupportedDevice = errors.New("solix: unsupported device model")

// ErrNegotiationTimeout is returned when the ECDH negotiation does not
// complete within the allowed time.
var ErrNegotiationTimeout = errors.New("solix: negotiation timed out")

// ErrChecksumMismatch is returned when a received packet has an invalid
// checksum.
var ErrChecksumMismatch = errors.New("solix: packet checksum mismatch")
