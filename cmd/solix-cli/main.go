// Command solix-cli provides a command-line interface for interacting with
// Anker Solix power stations over Bluetooth Low Energy.
//
// Usage (requires root or CAP_NET_RAW for BLE):
//
//	sudo solix-cli scan
//	sudo solix-cli status --addr E8:EE:CC:7C:0A:2A
//	sudo solix-cli monitor --addr E8:EE:CC:7C:0A:2A
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix"
	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:  "solix-cli",
		Usage: "Anker Solix BLE command-line interface",
		Commands: []*cli.Command{
			scanCommand(),
			statusCommand(),
			monitorCommand(),
			setCommand(),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// ---- scan ---------------------------------------------------------------

func scanCommand() *cli.Command {
	return &cli.Command{
		Name:  "scan",
		Usage: "Scan for nearby Solix devices and print their addresses",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:    "scan-timeout",
				Value:   10 * time.Second,
				Usage:   "How long to scan for devices",
				Sources: cli.EnvVars("SOLIX_SCAN_TIMEOUT"),
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Output results as JSON",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, err := solix.NewClient(solix.Config{
				ScanTimeout: cmd.Duration("scan-timeout"),
			})
			if err != nil {
				return fmt.Errorf("failed to initialise BLE: %w", err)
			}

			fmt.Fprintln(os.Stderr, "Scanning for Solix devices...")
			devices, err := client.Scan(ctx)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			if len(devices) == 0 {
				fmt.Fprintln(os.Stderr, "No Solix devices found.")
				return nil
			}

			if cmd.Bool("json") {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(devices)
				return nil
			}

			fmt.Printf("Found %d device(s):\n", len(devices))
			for i, d := range devices {
				fmt.Printf("  [%d] %-24s  addr=%-18s  rssi=%d\n", i+1, d.Name, d.Addr, d.RSSI)
			}
			return nil
		},
	}
}

// ---- status -------------------------------------------------------------

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Connect to a device and print a single telemetry snapshot",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "addr",
				Usage:   "Device MAC address (or omit to auto-discover)",
				Sources: cli.EnvVars("SOLIX_ADDRESS"),
			},
			&cli.DurationFlag{
				Name:    "scan-timeout",
				Value:   10 * time.Second,
				Usage:   "How long to scan when auto-discovering",
				Sources: cli.EnvVars("SOLIX_SCAN_TIMEOUT"),
			},
			&cli.DurationFlag{
				Name:    "connect-timeout",
				Value:   5 * time.Second,
				Usage:   "BLE connection timeout",
				Sources: cli.EnvVars("SOLIX_CONNECT_TIMEOUT"),
			},
			&cli.DurationFlag{
				Name:    "nego-timeout",
				Value:   5 * time.Second,
				Usage:   "ECDH negotiation timeout",
				Sources: cli.EnvVars("SOLIX_NEGO_TIMEOUT"),
			},
			&cli.DurationFlag{
				Name:    "wait-timeout",
				Value:   10 * time.Second,
				Usage:   "How long to wait for first telemetry",
				Sources: cli.EnvVars("SOLIX_WAIT_TIMEOUT"),
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Output result as JSON",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, err := solix.NewClient(solix.Config{
				ScanTimeout:        cmd.Duration("scan-timeout"),
				ConnectTimeout:     cmd.Duration("connect-timeout"),
				NegotiationTimeout: cmd.Duration("nego-timeout"),
				Logger:             slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
			})
			if err != nil {
				return fmt.Errorf("failed to initialise BLE: %w", err)
			}

			targetAddr := cmd.String("addr")
			if targetAddr == "" {
				targetAddr, err = autoDiscover(ctx, client)
				if err != nil {
					return err
				}
			}

			device, err := connect(ctx, client, targetAddr)
			if err != nil {
				return err
			}
			defer device.Disconnect()

			fmt.Println("Waiting for telemetry data...")
			status, err := waitForStatus(ctx, device, cmd.Duration("wait-timeout"))
			if err != nil {
				return err
			}

			if cmd.Bool("json") {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(status)
				return nil
			}

			printStatus(status)
			return nil
		},
	}
}

// ---- monitor ------------------------------------------------------------

func monitorCommand() *cli.Command {
	return &cli.Command{
		Name:  "monitor",
		Usage: "Connect to a device and stream telemetry updates until interrupted",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "addr",
				Usage:   "Device MAC address (or omit to auto-discover)",
				Sources: cli.EnvVars("SOLIX_ADDRESS"),
			},
			&cli.DurationFlag{
				Name:    "scan-timeout",
				Value:   10 * time.Second,
				Usage:   "How long to scan when auto-discovering",
				Sources: cli.EnvVars("SOLIX_SCAN_TIMEOUT"),
			},
			&cli.DurationFlag{
				Name:    "connect-timeout",
				Value:   30 * time.Second,
				Usage:   "BLE connection timeout",
				Sources: cli.EnvVars("SOLIX_CONNECT_TIMEOUT"),
			},
			&cli.DurationFlag{
				Name:    "nego-timeout",
				Value:   90 * time.Second,
				Usage:   "ECDH negotiation timeout",
				Sources: cli.EnvVars("SOLIX_NEGO_TIMEOUT"),
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Output each update as a JSON object",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, err := solix.NewClient(solix.Config{
				ScanTimeout:        cmd.Duration("scan-timeout"),
				ConnectTimeout:     cmd.Duration("connect-timeout"),
				NegotiationTimeout: cmd.Duration("nego-timeout"),
				Logger:             slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
			})
			if err != nil {
				return fmt.Errorf("failed to initialise BLE: %w", err)
			}

			targetAddr := cmd.String("addr")
			if targetAddr == "" {
				targetAddr, err = autoDiscover(ctx, client)
				if err != nil {
					return err
				}
			}

			device, err := connect(ctx, client, targetAddr)
			if err != nil {
				return err
			}
			defer device.Disconnect()

			fmt.Fprintf(os.Stderr, "Monitoring %s (Ctrl-C to exit)...\n", targetAddr)

			outputJSON := cmd.Bool("json")
			device.AddCallback(func(status solix.DeviceStatus) {
				if outputJSON {
					enc := json.NewEncoder(os.Stdout)
					_ = enc.Encode(status)
					return
				}
				fmt.Printf("[%s] battery=%d%%  solar=%dW  ac_in=%dW  ac_out=%dW  temp=%d°C\n",
					status.UpdatedAt.Format("15:04:05"),
					status.BatteryPercent,
					status.SolarPowerIn,
					status.ACPowerIn,
					status.ACPowerOut,
					status.Temperature,
				)
			})

			select {
			case <-ctx.Done():
				fmt.Fprintln(os.Stderr, "Shutting down...")
			case <-device.Disconnected():
				fmt.Fprintln(os.Stderr, "Device disconnected.")
			}
			return nil
		},
	}
}

// ---- helpers ------------------------------------------------------------

// autoDiscover scans for Solix devices and returns the first device's address.
func autoDiscover(ctx context.Context, client *solix.Client) (string, error) {
	fmt.Fprintln(os.Stderr, "No --addr specified, scanning for Solix devices...")
	devices, err := client.Scan(ctx)
	if err != nil {
		return "", fmt.Errorf("scan failed: %w", err)
	}
	if len(devices) == 0 {
		return "", fmt.Errorf("no Solix devices found")
	}
	fmt.Printf("Found %d device(s), using first: %s (%s)\n", len(devices), devices[0].Name, devices[0].Addr)
	return devices[0].Addr, nil
}

// connect connects to addr, retrying once after a brief scan on failure.
func connect(ctx context.Context, client *solix.Client, addr string) (*solix.Device, error) {
	fmt.Fprintf(os.Stderr, "Connecting to %s...\n", addr)
	device, err := client.Connect(ctx, addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connection failed (%v), attempting brief scan to wake up device...\n", err)
		// Perform a short scan to populate macOS BLE cache
		scanCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, _ = client.Scan(scanCtx)
		cancel()

		fmt.Fprintf(os.Stderr, "Retrying connection to %s...\n", addr)
		device, err = client.Connect(ctx, addr)
		if err != nil {
			return nil, fmt.Errorf("connect failed: %w", err)
		}
	}
	fmt.Fprintf(os.Stderr, "Connected to %s (%s)\n", device.Name(), device.Addr())
	return device, nil
}

// waitForStatus polls device.Status until data is available or waitTimeout elapses.
func waitForStatus(ctx context.Context, device *solix.Device, waitTimeout time.Duration) (solix.DeviceStatus, error) {
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	for {
		select {
		case <-waitCtx.Done():
			return solix.DeviceStatus{}, fmt.Errorf("timed out waiting for telemetry data")
		case <-time.After(500 * time.Millisecond):
			status, err := device.Status(ctx)
			if err == solix.ErrNoData {
				continue
			}
			if err != nil {
				return solix.DeviceStatus{}, fmt.Errorf("status read failed: %w", err)
			}
			return status, nil
		}
	}
}

// printStatus prints a human-readable telemetry snapshot to stdout.
func printStatus(s solix.DeviceStatus) {
	fmt.Printf("Model:                    %s\n", s.Model)
	fmt.Printf("Serial Number:            %s\n", s.SerialNumber)
	fmt.Printf("Firmware:                 %s\n", s.SoftwareVersion)
	fmt.Printf("Battery:                  %d%%\n", s.BatteryPercent)
	if s.BatteryHealth > 0 {
		fmt.Printf("Battery Health:           %d%%\n", s.BatteryHealth)
	}
	if s.NumExpansion > 0 {
		fmt.Printf("Expansion Battery:        %d%%\n", s.BatteryPercentExpansion)
	}
	fmt.Printf("Time Remaining:           %.1fh (%d days)\n", s.TimeRemainingHours, s.DaysRemaining)
	fmt.Printf("Solar Power In:           %d W\n", s.SolarPowerIn)
	fmt.Printf("AC Power In:              %d W\n", s.ACPowerIn)
	fmt.Printf("AC Power Out:             %d W\n", s.ACPowerOut)
	if s.DC1PowerOut > 0 || s.DC2PowerOut > 0 {
		fmt.Printf("DC Power Out (1/2):       %d W / %d W\n", s.DC1PowerOut, s.DC2PowerOut)
	}
	if s.USBC1Power > 0 || s.USBC2Power > 0 || s.USBC3Power > 0 {
		fmt.Printf("USB-C Power (1/2/3):      %d W / %d W / %d W\n", s.USBC1Power, s.USBC2Power, s.USBC3Power)
	}
	if s.USBA1Power > 0 || s.USBA2Power > 0 {
		fmt.Printf("USB-A Power (1/2):        %d W / %d W\n", s.USBA1Power, s.USBA2Power)
	}
	fmt.Printf("Temperature:              %d °C\n", s.Temperature)
	fmt.Printf("Last Updated:             %s\n", s.UpdatedAt.Format(time.RFC3339))
}

// ---- set ----------------------------------------------------------------

// setCommand returns the top-level "set" command which groups all control
// sub-commands. Each sub-command connects to the device, sends a single
// command, and disconnects.
func setCommand() *cli.Command {
	// commonFlags are shared across all set sub-commands.
	commonFlags := []cli.Flag{
		&cli.StringFlag{
			Name:    "addr",
			Usage:   "Device MAC address (or omit to auto-discover)",
			Sources: cli.EnvVars("SOLIX_ADDRESS"),
		},
		&cli.DurationFlag{
			Name:    "scan-timeout",
			Value:   10 * time.Second,
			Usage:   "How long to scan when auto-discovering",
			Sources: cli.EnvVars("SOLIX_SCAN_TIMEOUT"),
		},
		&cli.DurationFlag{
			Name:    "connect-timeout",
			Value:   15 * time.Second,
			Usage:   "BLE connection timeout",
			Sources: cli.EnvVars("SOLIX_CONNECT_TIMEOUT"),
		},
		&cli.DurationFlag{
			Name:    "nego-timeout",
			Value:   30 * time.Second,
			Usage:   "ECDH negotiation timeout",
			Sources: cli.EnvVars("SOLIX_NEGO_TIMEOUT"),
		},
	}

	// connectAndSend is a helper that connects, runs fn with the device, then
	// disconnects.  All sub-command actions delegate to this.
	connectAndSend := func(ctx context.Context, cmd *cli.Command, fn func(context.Context, *solix.Device) error) error {
		client, err := solix.NewClient(solix.Config{
			ScanTimeout:        cmd.Duration("scan-timeout"),
			ConnectTimeout:     cmd.Duration("connect-timeout"),
			NegotiationTimeout: cmd.Duration("nego-timeout"),
			Logger:             slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		})
		if err != nil {
			return fmt.Errorf("failed to initialise BLE: %w", err)
		}

		targetAddr := cmd.String("addr")
		if targetAddr == "" {
			targetAddr, err = autoDiscover(ctx, client)
			if err != nil {
				return err
			}
		}

		device, err := connect(ctx, client, targetAddr)
		if err != nil {
			return err
		}
		defer device.Disconnect()

		return fn(ctx, device)
	}

	return &cli.Command{
		Name:  "set",
		Usage: "Send a control command to a Solix device",
		Commands: []*cli.Command{
			{
				Name:  "ac-output",
				Usage: "Turn the AC inverter output on or off (usage: ac-output <on|off>)",
				Flags: commonFlags,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					on, err := parseOnOff(cmd)
					if err != nil {
						return err
					}
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetACOutput(ctx, on); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "AC output %s\n", onOffStr(on))
						return nil
					})
				},
			},
			{
				Name:  "dc-output",
				Usage: "Turn the DC / 12 V output on or off (usage: dc-output <on|off>)",
				Flags: commonFlags,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					on, err := parseOnOff(cmd)
					if err != nil {
						return err
					}
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetDCOutput(ctx, on); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "DC output %s\n", onOffStr(on))
						return nil
					})
				},
			},
			{
				Name:  "ac-charge-rate",
				Usage: "Set the AC charging power limit in watts (F2600 only, range 100–1440 W)",
				Flags: append(commonFlags, &cli.IntFlag{
					Name:     "watts",
					Required: true,
					Usage:    "Charging power in watts (100–1440)",
				}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					watts := cmd.Int("watts")
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetACChargePower(ctx, int(watts)); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "AC charge rate set to %d W\n", watts)
						return nil
					})
				},
			},
			{
				Name:  "display-timeout",
				Usage: "Set the display auto-off timeout in seconds (0 = always on)",
				Flags: append(commonFlags, &cli.IntFlag{
					Name:     "seconds",
					Required: true,
					Usage:    "Timeout in seconds (0–65535); 0 keeps the display always on",
				}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					secs := cmd.Int("seconds")
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetDisplayTimeout(ctx, int(secs)); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "Display timeout set to %d s\n", secs)
						return nil
					})
				},
			},
			{
				Name:  "display-brightness",
				Usage: "Set the display brightness level (0=off, 1=low, 2=medium, 3=high)",
				Flags: append(commonFlags, &cli.IntFlag{
					Name:     "level",
					Required: true,
					Usage:    "Brightness level (0–3)",
				}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					level := cmd.Int("level")
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetDisplayBrightness(ctx, int(level)); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "Display brightness set to %d\n", level)
						return nil
					})
				},
			},
			{
				Name:  "led",
				Usage: "Set the LED light bar mode (0=off, 1=low, 2=medium, 3=high, 4=SOS)",
				Flags: append(commonFlags, &cli.IntFlag{
					Name:     "level",
					Required: true,
					Usage:    "LED mode (0–4)",
				}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					level := cmd.Int("level")
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetLED(ctx, int(level)); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "LED level set to %d\n", level)
						return nil
					})
				},
			},
			{
				Name:  "display",
				Usage: "Turn the display on or off (usage: display <on|off>)",
				Flags: commonFlags,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					on, err := parseOnOff(cmd)
					if err != nil {
						return err
					}
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetDisplay(ctx, on); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "Display %s\n", onOffStr(on))
						return nil
					})
				},
			},
			{
				Name:  "power-saving",
				Usage: "Enable or disable power saving mode (usage: power-saving <on|off>)",
				Flags: commonFlags,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					on, err := parseOnOff(cmd)
					if err != nil {
						return err
					}
					return connectAndSend(ctx, cmd, func(ctx context.Context, d *solix.Device) error {
						if err := d.SetPowerSaving(ctx, on); err != nil {
							return err
						}
						fmt.Fprintf(os.Stderr, "Power saving %s\n", onOffStr(on))
						return nil
					})
				},
			},
		},
	}
}

// parseOnOff reads the first positional argument and returns true for "on",
// false for "off", or an error if the argument is missing or unrecognised.
func parseOnOff(cmd *cli.Command) (bool, error) {
	arg := cmd.Args().First()
	switch arg {
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected 'on' or 'off', got %q", arg)
	}
}

// onOffStr returns "on" or "off" for use in status messages.
func onOffStr(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
