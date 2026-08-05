// Package solix implements a Go client for communicating with Anker Solix
// power stations over Bluetooth Low Energy.
//
// # Overview
//
// The package provides two protocol paths:
//
//   - Encrypted protocol (modern firmware, most devices): uses ECDH key exchange
//     followed by AES-128-CBC encryption on GATT characteristics with the
//     8c8500xx UUID family.
//
//   - Unencrypted protocol (F2000 / 767 PowerHouse, older firmware): uses a
//     simpler binary framing over GATT characteristics 00007777 (write) and
//     00008888 (notify).
//
// # Quick Start
//
//	cfg := solix.Config{ScanTimeout: 10 * time.Second}
//	client, err := solix.NewClient(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	ctx := context.Background()
//	devices, err := client.Scan(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	device, err := client.Connect(ctx, devices[0].Addr)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer device.Disconnect()
//
//	status, err := device.Status(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Battery: %d%%\n", status.BatteryPercent)
package solix
