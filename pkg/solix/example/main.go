//go:build ignore

// Command solix-monitor demonstrates discovering, connecting, and reading
// telemetry from an Anker Solix power station.
//
// Usage (requires root or CAP_NET_RAW for BLE):
//
//	sudo go run pkg/solix/example/main.go
//	sudo go run pkg/solix/example/main.go -addr E8:EE:CC:7C:0A:2A
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix"
)

func main() {
	addr := flag.String("addr", "", "Device MAC address to connect to directly (skips scan)")
	scanTimeout := flag.Duration("scan-timeout", 10*time.Second, "Scan duration")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := solix.Config{
		ScanTimeout:        *scanTimeout,
		ConnectTimeout:     30 * time.Second,
		NegotiationTimeout: 90 * time.Second,
		Logger:             logger,
	}

	client, err := solix.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise BLE: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var targetAddr string

	if *addr != "" {
		targetAddr = *addr
	} else {
		fmt.Println("Scanning for Solix devices...")
		devices, err := client.Scan(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
			os.Exit(1)
		}
		if len(devices) == 0 {
			fmt.Println("No Solix devices found.")
			os.Exit(0)
		}
		fmt.Printf("Found %d device(s):\n", len(devices))
		for i, d := range devices {
			fmt.Printf("  [%d] %s  (%s)  RSSI: %d\n", i+1, d.Name, d.Addr, d.RSSI)
		}
		targetAddr = devices[0].Addr
	}

	fmt.Printf("Connecting to %s...\n", targetAddr)
	device, err := client.Connect(ctx, targetAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Connected to %s (%s)\n", device.Name(), device.Addr())
	defer device.Disconnect()

	// Register callback to print every telemetry update
	device.AddCallback(func(status solix.DeviceStatus) {
		fmt.Printf("[%s] Battery: %d%%  Solar: %dW  AC Out: %dW  Temp: %d°C\n",
			status.UpdatedAt.Format("15:04:05"),
			status.BatteryPercent,
			status.SolarPowerIn,
			status.ACPowerOut,
			status.Temperature,
		)
	})

	// Also try a one-shot status read
	pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		select {
		case <-pollCtx.Done():
			fmt.Println("Timeout waiting for telemetry")
			os.Exit(1)
		case <-time.After(500 * time.Millisecond):
			status, err := device.Status(ctx)
			if err == solix.ErrNoData {
				continue
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "status error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("\nInitial telemetry snapshot:\n")
			fmt.Printf("  Model:          %s\n", status.Model)
			fmt.Printf("  Serial:         %s\n", status.SerialNumber)
			fmt.Printf("  Battery:        %d%%\n", status.BatteryPercent)
			fmt.Printf("  Battery Health: %d%%\n", status.BatteryHealth)
			fmt.Printf("  Time Remaining: %.1f h (%d days, %.1f h)\n",
				status.TimeRemainingHours, status.DaysRemaining, status.HoursRemaining)
			fmt.Printf("  Solar In:       %d W\n", status.SolarPowerIn)
			fmt.Printf("  AC In:          %d W\n", status.ACPowerIn)
			fmt.Printf("  AC Out:         %d W\n", status.ACPowerOut)
			fmt.Printf("  Temperature:    %d °C\n", status.Temperature)
			fmt.Printf("  Firmware:       %s\n", status.SoftwareVersion)
			cancel()
			goto monitorLoop
		}
	}

monitorLoop:
	fmt.Println("\nMonitoring (Ctrl-C to exit)...")
	select {
	case <-ctx.Done():
		fmt.Println("Shutting down...")
	case <-device.Disconnected():
		fmt.Println("Device disconnected")
	}
}
