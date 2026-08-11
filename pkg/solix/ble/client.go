// Package ble provides a thin wrapper around github.com/go-ble/ble for
// connecting to Anker Solix devices on Linux.
//
// On Linux, go-ble uses the kernel HCI socket interface directly so no
// external tools (bluetoothctl, hcitool) are required.
package ble

import (
	"context"
	"fmt"
	"time"

	"github.com/go-ble/ble"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix/protocol"
)

// Client wraps the go-ble device for Solix BLE operations.
type Client struct {
	device ble.Device
}

// Connection represents an active BLE connection to a Solix device.
type Connection struct {
	client *ble.Client
	addr   string
}

// NewClient initialises the BLE device.
// It must be called once per process and the returned Client is safe for
// concurrent use after initialisation.
func NewClient() (*Client, error) {
	d, err := NewDevice()
	if err != nil {
		return nil, fmt.Errorf("ble: failed to initialise BLE device: %w", err)
	}
	ble.SetDefaultDevice(d)
	return &Client{device: d}, nil
}

// Scan scans for nearby Solix BLE devices. It calls handler for each
// discovered device that advertises the Solix identifier service UUID.
// Scanning stops when ctx is cancelled.
func (c *Client) Scan(ctx context.Context, handler func(addr, name string)) error {
	filter := func(a ble.Advertisement) bool {
		if a.LocalName() == "767_PowerHouse" {
			return true
		}
		for _, svcUUID := range a.Services() {
			if svcUUID.Equal(ble.MustParse(protocol.UUIDIdentifier)) || svcUUID.Equal(ble.MustParse("ff09")) {
				return true
			}
		}
		return false
	}

	advHandler := func(a ble.Advertisement) {
		handler(a.Addr().String(), a.LocalName())
	}

	return ble.Scan(ctx, false, advHandler, filter)
}

// Connect establishes a BLE connection to the device at addr (e.g.
// "E8:EE:CC:7C:0A:2A"). Returns an open connection or an error.
func (c *Client) Connect(ctx context.Context, addr string) (*Connection, error) {
	a := ble.NewAddr(addr)
	conn, err := ble.Dial(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("ble: connect to %s: %w", addr, err)
	}
	return &Connection{client: &conn, addr: addr}, nil
}

// WriteChar writes bytes to the GATT characteristic identified by uuid.
func (c *Connection) WriteChar(ctx context.Context, uuid string, data []byte) error {
	profile, err := (*c.client).DiscoverProfile(true)
	if err != nil {
		return fmt.Errorf("ble: discover profile: %w", err)
	}

	char := profile.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(uuid)))
	if char == nil {
		return fmt.Errorf("ble: characteristic %s not found", uuid)
	}

	return (*c.client).WriteCharacteristic(char, data, true)
}

// Subscribe registers a notification handler for the GATT characteristic
// identified by uuid and returns a cancel func to unsubscribe.
func (c *Connection) Subscribe(ctx context.Context, uuid string, handler func([]byte)) error {
	profile, err := (*c.client).DiscoverProfile(true)
	if err != nil {
		return fmt.Errorf("ble: discover profile: %w", err)
	}

	char := profile.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(uuid)))
	if char == nil {
		return fmt.Errorf("ble: characteristic %s not found", uuid)
	}

	return (*c.client).Subscribe(char, false, handler)
}

// Disconnect closes the BLE connection.
func (c *Connection) Disconnect() error {
	return (*c.client).CancelConnection()
}

// Addr returns the device MAC address.
func (c *Connection) Addr() string { return c.addr }

// DiscoveredDevice holds information about a BLE device found during scanning.
type DiscoveredDevice struct {
	Addr string
	Name string
	RSSI int
	// SeenAt records when the advertisement was received.
	SeenAt time.Time
}
