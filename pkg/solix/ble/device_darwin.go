//go:build darwin
// +build darwin

package ble

import (
	"github.com/go-ble/ble"
	"github.com/go-ble/ble/darwin"
)

func NewDevice() (ble.Device, error) {
	return darwin.NewDevice()
}
