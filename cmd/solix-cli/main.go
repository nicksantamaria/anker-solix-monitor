// Command solix-cli provides a command-line interface for interacting with
// Anker Solix power stations over Bluetooth Low Energy.
//
// Usage (requires root or CAP_NET_RAW for BLE):
//
//	sudo solix-cli scan
//	sudo solix-cli status -addr E8:EE:CC:7C:0A:2A
//	sudo solix-cli monitor -addr E8:EE:CC:7C:0A:2A
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix"
)

const usage = `solix-cli - Anker Solix BLE command-line interface

Usage:
  solix-cli <command> [flags]

Commands:
  scan      Scan for nearby Solix devices and print their addresses.
  status    Connect to a device and print a single telemetry snapshot.
  monitor   Connect to a device and stream telemetry updates until interrupted.

Global flags:
  -log-level string   Log level: debug, info, warn, error (default "warn")

Run 'solix-cli <command> -help' for command-specific flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "scan":
		runScan(args)
	case "status":
		runStatus(args)
	case "monitor":
		runMonitor(args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(1)
	}
}

// ---- scan ---------------------------------------------------------------

func runScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	scanTimeout := fs.Duration("scan-timeout", 10*time.Second, "How long to scan for devices")
	outputJSON := fs.Bool("json", false, "Output results as JSON")
	_ = fs.Parse(args)

	client, err := solix.NewClient(solix.Config{
		ScanTimeout: *scanTimeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialise BLE: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintln(os.Stderr, "Scanning for Solix devices...")
	devices, err := client.Scan(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: scan failed: %v\n", err)
		os.Exit(1)
	}

	if len(devices) == 0 {
		fmt.Fprintln(os.Stderr, "No Solix devices found.")
		os.Exit(0)
	}

	if *outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(devices)
		return
	}

	fmt.Printf("Found %d device(s):\n", len(devices))
	for i, d := range devices {
		fmt.Printf("  [%d] %-24s  addr=%-18s  rssi=%d\n", i+1, d.Name, d.Addr, d.RSSI)
	}
}

// ---- status -------------------------------------------------------------

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	addr := fs.String("addr", "", "Device MAC address (required, or omit to auto-discover)")
	scanTimeout := fs.Duration("scan-timeout", 10*time.Second, "How long to scan when auto-discovering")
	connectTimeout := fs.Duration("connect-timeout", 5*time.Second, "BLE connection timeout")
	negoTimeout := fs.Duration("nego-timeout", 5*time.Second, "ECDH negotiation timeout")
	waitTimeout := fs.Duration("wait-timeout", 10*time.Second, "How long to wait for first telemetry")
	outputJSON := fs.Bool("json", false, "Output result as JSON")
	_ = fs.Parse(args)

	client, err := solix.NewClient(solix.Config{
		ScanTimeout:        *scanTimeout,
		ConnectTimeout:     *connectTimeout,
		NegotiationTimeout: *negoTimeout,
		Logger:             slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialise BLE: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	targetAddr := *addr
	if targetAddr == "" {
		targetAddr = mustAutoDiscover(ctx, client)
	}

	device := mustConnect(ctx, client, targetAddr)
	defer device.Disconnect()

	fmt.Println("Waiting for telemetry data...")
	status := mustWaitForStatus(ctx, device, *waitTimeout)

	if *outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(status)
		return
	}

	printStatus(status)
}

// ---- monitor ------------------------------------------------------------

func runMonitor(args []string) {
	fs := flag.NewFlagSet("monitor", flag.ExitOnError)
	addr := fs.String("addr", "", "Device MAC address (required, or omit to auto-discover)")
	scanTimeout := fs.Duration("scan-timeout", 10*time.Second, "How long to scan when auto-discovering")
	connectTimeout := fs.Duration("connect-timeout", 30*time.Second, "BLE connection timeout")
	negoTimeout := fs.Duration("nego-timeout", 90*time.Second, "ECDH negotiation timeout")
	outputJSON := fs.Bool("json", false, "Output each update as a JSON object")
	_ = fs.Parse(args)

	client, err := solix.NewClient(solix.Config{
		ScanTimeout:        *scanTimeout,
		ConnectTimeout:     *connectTimeout,
		NegotiationTimeout: *negoTimeout,
		Logger:             slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to initialise BLE: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	targetAddr := *addr
	if targetAddr == "" {
		targetAddr = mustAutoDiscover(ctx, client)
	}

	device := mustConnect(ctx, client, targetAddr)
	defer device.Disconnect()

	fmt.Fprintf(os.Stderr, "Monitoring %s (Ctrl-C to exit)...\n", targetAddr)

	device.AddCallback(func(status solix.DeviceStatus) {
		if *outputJSON {
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
}

// ---- helpers ------------------------------------------------------------

// mustAutoDiscover scans for Solix devices and returns the first device's
// address, exiting the process if none are found.
func mustAutoDiscover(ctx context.Context, client *solix.Client) string {
	fmt.Fprintln(os.Stderr, "No -addr specified, scanning for Solix devices...")
	devices, err := client.Scan(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: scan failed: %v\n", err)
		os.Exit(1)
	}
	if len(devices) == 0 {
		fmt.Fprintln(os.Stderr, "No Solix devices found.")
		os.Exit(1)
	}
	fmt.Printf("Found %d device(s), using first: %s (%s)\n", len(devices), devices[0].Name, devices[0].Addr)
	return devices[0].Addr
}

// mustConnect connects to addr and exits on error.
func mustConnect(ctx context.Context, client *solix.Client, addr string) *solix.Device {
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
			fmt.Fprintf(os.Stderr, "error: connect failed: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Fprintf(os.Stderr, "Connected to %s (%s)\n", device.Name(), device.Addr())
	return device
}

// mustWaitForStatus polls device.Status until data is available or waitTimeout elapses.
func mustWaitForStatus(ctx context.Context, device *solix.Device, waitTimeout time.Duration) solix.DeviceStatus {
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	for {
		select {
		case <-waitCtx.Done():
			fmt.Fprintln(os.Stderr, "error: timed out waiting for telemetry data")
			os.Exit(1)
		case <-time.After(500 * time.Millisecond):
			status, err := device.Status(ctx)
			if err == solix.ErrNoData {
				continue
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: status read failed: %v\n", err)
				os.Exit(1)
			}
			return status
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
