package solix

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix/protocol"
)

// SetACOutput turns the AC inverter output on or off.
//
// Applies to gen1 devices: C300, C800, C1000, F2600, F3800.
// For C1000 Gen2 use SendCommand with protocol.CmdACOutputGen2.
func (d *Device) SetACOutput(ctx context.Context, on bool) error {
	cmd, err := hex.DecodeString(protocol.CmdACOutput)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadOnOff(on))
}

// SetDCOutput turns the DC / 12 V output on or off.
//
// Applies to gen1 devices: C300, C800, C1000, F2600, F3800, C300DC.
// For C1000 Gen2 use SendCommand with protocol.CmdDCOutputGen2.
func (d *Device) SetDCOutput(ctx context.Context, on bool) error {
	cmd, err := hex.DecodeString(protocol.CmdDCOutput)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadOnOff(on))
}

// SetACChargePower sets the AC charging power limit in watts.
//
// The valid range is 100–1440 W. This command is only supported by the F2600;
// other models will accept the packet but may ignore it.
func (d *Device) SetACChargePower(ctx context.Context, watts int) error {
	if watts < 100 || watts > 1440 {
		return fmt.Errorf("AC charge power must be between 100 and 1440 W (got %d)", watts)
	}
	cmd, err := hex.DecodeString(protocol.CmdACChargePower)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadUint16(uint16(watts)))
}

// SetDisplayTimeout sets the display auto-off timeout in seconds.
// Pass 0 to keep the display always on.
func (d *Device) SetDisplayTimeout(ctx context.Context, seconds int) error {
	if seconds < 0 || seconds > 65535 {
		return fmt.Errorf("display timeout must be between 0 and 65535 seconds (got %d)", seconds)
	}
	cmd, err := hex.DecodeString(protocol.CmdDisplayTimeout)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadUint16(uint16(seconds)))
}

// SetDisplayBrightness sets the display brightness level.
//
//   - 0 = off
//   - 1 = low
//   - 2 = medium
//   - 3 = high
func (d *Device) SetDisplayBrightness(ctx context.Context, level int) error {
	if level < 0 || level > 3 {
		return fmt.Errorf("display brightness must be between 0 and 3 (got %d)", level)
	}
	cmd, err := hex.DecodeString(protocol.CmdDisplayBrightness)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadLevel(byte(level)))
}

// SetLED sets the LED light bar mode.
//
//   - 0 = off
//   - 1 = low
//   - 2 = medium
//   - 3 = high
//   - 4 = SOS
func (d *Device) SetLED(ctx context.Context, level int) error {
	if level < 0 || level > 4 {
		return fmt.Errorf("LED level must be between 0 and 4 (got %d)", level)
	}
	cmd, err := hex.DecodeString(protocol.CmdLEDMode)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadLevel(byte(level)))
}

// SetDisplay turns the display on or off.
func (d *Device) SetDisplay(ctx context.Context, on bool) error {
	cmd, err := hex.DecodeString(protocol.CmdDisplayOnOff)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadOnOff(on))
}

// SetPowerSaving enables or disables power saving mode.
func (d *Device) SetPowerSaving(ctx context.Context, on bool) error {
	cmd, err := hex.DecodeString(protocol.CmdPowerSaving)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	return d.SendCommand(ctx, cmd, protocol.BuildPayloadOnOff(on))
}
