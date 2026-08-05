package models

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// F2000Status holds the decoded telemetry for an F2000(P) / 767 PowerHouse.
// Field names use the parameter key from the Python reference (e.g. "a4" →
// TimeRemainingHours). Unknown/unavailable values are represented as -1 for
// numeric fields and empty string for string fields.
type F2000Status struct {
	// TimeRemainingHours is the total hours remaining to full/empty.
	TimeRemainingHours float64
	// DaysRemaining is the integer number of complete days remaining.
	DaysRemaining int
	// HoursRemaining is the fractional hours within the current day.
	HoursRemaining float64
	// TimestampRemaining is the estimated wall-clock time of full/empty.
	TimestampRemaining *time.Time

	// Power readings in Watts
	ACToBattery          int // param a5
	ACPowerOutSockets    int // param a6
	USBC1Power           int // param a7
	USBC2Power           int // param a8
	USBC3Power           int // param a9
	USBA1Power           int // param aa
	USBA2Power           int // param ab
	DC1PowerOut          int // param ac
	DC2PowerOut          int // param ad
	SolarPowerIn         int // param ae
	ACPowerIn            int // param af
	ACPowerOut           int // param b0

	// Firmware versions
	SoftwareVersion           string // param b3
	SoftwareVersionExpansion  string // param b9
	SoftwareVersionController string // param ba

	// Temperature in degrees Celsius (signed)
	Temperature          int // param bd
	TemperatureExpansion int // param be

	// Battery state
	BatteryPercent          int // param c1
	BatteryPercentExpansion int // param c2
	BatteryHealth           int // param c3
	BatteryHealthExpansion  int // param c4
	NumExpansion            int // param c5

	// Device info
	SerialNumber string // param d0
}

// F2000 implements Device for the F2000(P) power station.
type F2000 struct {
	status F2000Status
}

func (f *F2000) Model() DeviceModel { return ModelF2000 }

func (f *F2000) ExpectedTelemetryLength() int { return 253 }

// Status returns the most recently decoded telemetry snapshot.
func (f *F2000) Status() F2000Status { return f.status }

// ParseTelemetry populates the status fields from the raw parameter map.
func (f *F2000) ParseTelemetry(params map[string][]byte) error {
	s := &f.status

	// Time remaining (param a4, bytes[1:])
	if v := parseIntParam(params, "a4", 1, -1, false); v >= 0 {
		s.TimeRemainingHours = float64(v) / 10.0
		days := int(s.TimeRemainingHours) / 24
		hours := s.TimeRemainingHours - float64(days*24)
		s.DaysRemaining = days
		s.HoursRemaining = roundTo1(hours)
		ts := time.Now().Add(time.Duration(s.TimeRemainingHours*float64(time.Hour)))
		s.TimestampRemaining = &ts
	}

	s.ACToBattery = parseIntParam(params, "a5", 1, -1, false)
	s.ACPowerOutSockets = parseIntParam(params, "a6", 1, -1, false)
	s.USBC1Power = parseIntParam(params, "a7", 1, -1, false)
	s.USBC2Power = parseIntParam(params, "a8", 1, -1, false)
	s.USBC3Power = parseIntParam(params, "a9", 1, -1, false)
	s.USBA1Power = parseIntParam(params, "aa", 1, -1, false)
	s.USBA2Power = parseIntParam(params, "ab", 1, -1, false)
	s.DC1PowerOut = parseIntParam(params, "ac", 1, -1, false)
	s.DC2PowerOut = parseIntParam(params, "ad", 1, -1, false)
	s.SolarPowerIn = parseIntParam(params, "ae", 1, -1, false)
	s.ACPowerIn = parseIntParam(params, "af", 1, -1, false)
	s.ACPowerOut = parseIntParam(params, "b0", 1, -1, false)

	// Firmware version: join digits with "."
	if v := parseIntParam(params, "b3", 1, -1, false); v >= 0 {
		s.SoftwareVersion = joinDigits(v)
	}
	if v := parseIntParam(params, "b9", 1, -1, false); v >= 0 {
		s.SoftwareVersionExpansion = joinDigits(v)
	}
	if v := parseIntParam(params, "ba", 1, -1, false); v >= 0 {
		s.SoftwareVersionController = joinDigits(v)
	}

	s.Temperature = parseIntParam(params, "bd", 1, -1, true)
	s.TemperatureExpansion = parseIntParam(params, "be", 1, -1, true)

	s.BatteryPercent = parseIntParam(params, "c1", 1, -1, false)
	s.BatteryPercentExpansion = parseIntParam(params, "c2", 1, -1, false)
	s.BatteryHealth = parseIntParam(params, "c3", 1, -1, false)
	s.BatteryHealthExpansion = parseIntParam(params, "c4", 1, -1, false)
	s.NumExpansion = parseIntParam(params, "c5", 1, -1, false)

	// Serial number: ASCII bytes from index 1
	if raw, ok := params["d0"]; ok && len(raw) > 1 {
		s.SerialNumber = strings.TrimRight(string(raw[1:]), "\x00")
	}

	return nil
}

// String returns a human-readable summary of the status.
func (s F2000Status) String() string {
	return fmt.Sprintf(
		"F2000Status{Battery: %d%%, SolarIn: %dW, ACOut: %dW, Temp: %d°C, Serial: %s}",
		s.BatteryPercent, s.SolarPowerIn, s.ACPowerOut, s.Temperature, s.SerialNumber,
	)
}

// parseIntParam extracts a little-endian integer from params[key][begin:end].
// begin=-1 means from start; end=-1 means to end. Returns dflt if key absent
// or slice invalid.
func parseIntParam(params map[string][]byte, key string, begin, end int, signed bool) int {
	raw, ok := params[key]
	if !ok {
		return -1
	}
	if begin < 0 {
		begin = 0
	}
	if end < 0 || end > len(raw) {
		end = len(raw)
	}
	if begin > end || begin > len(raw) {
		return -1
	}
	slice := raw[begin:end]
	if len(slice) == 0 {
		return 0
	}
	var v int64
	switch len(slice) {
	case 1:
		if signed {
			v = int64(int8(slice[0]))
		} else {
			v = int64(slice[0])
		}
	case 2:
		u := binary.LittleEndian.Uint16(slice)
		if signed {
			v = int64(int16(u))
		} else {
			v = int64(u)
		}
	case 4:
		u := binary.LittleEndian.Uint32(slice)
		if signed {
			v = int64(int32(u))
		} else {
			v = int64(u)
		}
	default:
		// General little-endian decode
		var u uint64
		for i, b := range slice {
			u |= uint64(b) << (8 * i)
		}
		v = int64(u)
	}
	return int(v)
}

func joinDigits(n int) string {
	s := fmt.Sprintf("%d", n)
	parts := make([]string, len(s))
	for i, c := range s {
		parts[i] = string(c)
	}
	return strings.Join(parts, ".")
}

func roundTo1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
