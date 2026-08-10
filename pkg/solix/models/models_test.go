package models_test

import (
	"testing"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix/models"
)

// TestF2000ParseTelemetry verifies F2000 parameter parsing against known values.
func TestF2000ParseTelemetry(t *testing.T) {
	// Build a synthetic parameter map that mimics real F2000 telemetry.
	// Each value follows TLV: param_id → raw bytes where bytes[0] is the type
	// byte and bytes[1:] is the actual value (little-endian for integers).
	params := map[string][]byte{
		// a4: time_remaining = 120 (raw int × 10 → 12.0 hours)
		"a4": {0x02, 0x78, 0x00}, // type=uint16, value=120LE

		// ae: solar_power_in = 500W
		"ae": {0x02, 0xF4, 0x01}, // type=uint16, value=500LE

		// af: ac_power_in = 1000W
		"af": {0x02, 0xE8, 0x03}, // type=uint16, value=1000LE

		// b0: ac_power_out = 250W
		"b0": {0x02, 0xFA, 0x00}, // type=uint16, value=250LE

		// bd: temperature = 25°C (signed)
		"bd": {0x01, 0x19}, // type=uint8, value=25

		// c1: battery_percentage = 80%
		"c1": {0x01, 0x50}, // type=uint8, value=80

		// c3: battery_health = 98%
		"c3": {0x01, 0x62}, // type=uint8, value=98

		// d0: serial_number "ABC123"
		"d0": append([]byte{0x04}, []byte("ABC123")...),
	}

	f := &models.F2000{}
	if err := f.ParseTelemetry(params); err != nil {
		t.Fatalf("ParseTelemetry: %v", err)
	}
	s := f.Status()

	if s.TimeRemainingHours != 12.0 {
		t.Errorf("TimeRemainingHours = %.1f, want 12.0", s.TimeRemainingHours)
	}
	if s.SolarPowerIn != 500 {
		t.Errorf("SolarPowerIn = %d, want 500", s.SolarPowerIn)
	}
	if s.ACPowerIn != 1000 {
		t.Errorf("ACPowerIn = %d, want 1000", s.ACPowerIn)
	}
	if s.ACPowerOut != 250 {
		t.Errorf("ACPowerOut = %d, want 250", s.ACPowerOut)
	}
	if s.Temperature != 25 {
		t.Errorf("Temperature = %d, want 25", s.Temperature)
	}
	if s.BatteryPercent != 80 {
		t.Errorf("BatteryPercent = %d, want 80", s.BatteryPercent)
	}
	if s.BatteryHealth != 98 {
		t.Errorf("BatteryHealth = %d, want 98", s.BatteryHealth)
	}
	if s.SerialNumber != "ABC123" {
		t.Errorf("SerialNumber = %q, want ABC123", s.SerialNumber)
	}
}

// TestF2000MissingParams verifies that missing params return -1 (not a panic).
func TestF2000MissingParams(t *testing.T) {
	f := &models.F2000{}
	if err := f.ParseTelemetry(map[string][]byte{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := f.Status()

	if s.BatteryPercent != -1 {
		t.Errorf("BatteryPercent = %d, want -1 for missing param", s.BatteryPercent)
	}
	if s.SolarPowerIn != -1 {
		t.Errorf("SolarPowerIn = %d, want -1 for missing param", s.SolarPowerIn)
	}
}

// TestF2000NegativeTemperature verifies signed temperature decoding.
func TestF2000NegativeTemperature(t *testing.T) {
	params := map[string][]byte{
		// bd: temperature = -5°C
		// -5 as int8 = 0xFB
		"bd": {0x01, 0xFB},
	}
	f := &models.F2000{}
	_ = f.ParseTelemetry(params)
	s := f.Status()

	if s.Temperature != -5 {
		t.Errorf("Temperature = %d, want -5", s.Temperature)
	}
}

// TestF2000ExpectedTelemetryLength verifies the model constant.
func TestF2000ExpectedTelemetryLength(t *testing.T) {
	f := &models.F2000{}
	if f.ExpectedTelemetryLength() != 253 {
		t.Errorf("ExpectedTelemetryLength() = %d, want 253", f.ExpectedTelemetryLength())
	}
}

// TestF2000Model verifies model identification.
func TestF2000Model(t *testing.T) {
	f := &models.F2000{}
	if f.Model() != models.ModelF2000 {
		t.Errorf("Model() = %q, want %q", f.Model(), models.ModelF2000)
	}
}

// TestF2000TimeRemainingDays verifies days/hours decomposition.
func TestF2000TimeRemainingDays(t *testing.T) {
	// 250 = 25.0 hours → 1 day, 1.0 hour
	params := map[string][]byte{
		"a4": {0x02, 0xFA, 0x00}, // 250 LE
	}
	f := &models.F2000{}
	_ = f.ParseTelemetry(params)
	s := f.Status()

	if s.TimeRemainingHours != 25.0 {
		t.Errorf("TimeRemainingHours = %.1f, want 25.0", s.TimeRemainingHours)
	}
	if s.DaysRemaining != 1 {
		t.Errorf("DaysRemaining = %d, want 1", s.DaysRemaining)
	}
	if s.HoursRemaining != 1.0 {
		t.Errorf("HoursRemaining = %.1f, want 1.0", s.HoursRemaining)
	}
	if s.TimestampRemaining == nil {
		t.Error("TimestampRemaining should not be nil")
	}
}
