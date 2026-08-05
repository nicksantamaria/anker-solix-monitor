package solix

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix/models"
	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix/protocol"
)

// Config holds configuration for the Solix BLE client.
type Config struct {
	// ScanTimeout is how long to scan for devices (default: 10s).
	ScanTimeout time.Duration
	// ConnectTimeout is how long to wait for a BLE connection (default: 30s).
	ConnectTimeout time.Duration
	// NegotiationTimeout is how long to allow for the ECDH negotiation
	// (default: 90s).
	NegotiationTimeout time.Duration
	// Logger is the structured logger to use (default: slog.Default()).
	Logger *slog.Logger
}

func (c *Config) setDefaults() {
	if c.ScanTimeout == 0 {
		c.ScanTimeout = 10 * time.Second
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 30 * time.Second
	}
	if c.NegotiationTimeout == 0 {
		c.NegotiationTimeout = 90 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Client is the top-level entry point for discovering and connecting to Solix
// devices. It manages the underlying BLE hardware interface.
//
// A single Client instance should be created per process.
type Client struct {
	cfg Config
	dev ble.Device
	mu  sync.Mutex
}

// NewClient initialises the Linux HCI BLE stack and returns a ready-to-use
// Client. This call opens the HCI socket so it must be called with sufficient
// permissions (typically root or CAP_NET_RAW).
func NewClient(cfg Config) (*Client, error) {
	cfg.setDefaults()

	d, err := linux.NewDevice()
	if err != nil {
		return nil, fmt.Errorf("solix: failed to initialise BLE device: %w", err)
	}
	ble.SetDefaultDevice(d)

	return &Client{cfg: cfg, dev: d}, nil
}

// Scan performs a BLE scan and returns all Solix devices found within the
// configured ScanTimeout. Devices are identified by the Solix advertisement
// UUID (0000ff09-...).
func (c *Client) Scan(ctx context.Context) ([]DiscoveredDevice, error) {
	scanCtx, cancel := context.WithTimeout(ctx, c.cfg.ScanTimeout)
	defer cancel()

	var mu sync.Mutex
	var found []DiscoveredDevice

	filter := func(a ble.Advertisement) bool {
		for _, svcUUID := range a.Services() {
			if svcUUID.Equal(ble.MustParse(protocol.UUIDIdentifier)) {
				return true
			}
		}
		return false
	}

	handler := func(a ble.Advertisement) {
		mu.Lock()
		defer mu.Unlock()
		// Deduplicate by address
		for _, d := range found {
			if d.Addr == a.Addr().String() {
				return
			}
		}
		found = append(found, DiscoveredDevice{
			Addr:   a.Addr().String(),
			Name:   a.LocalName(),
			RSSI:   a.RSSI(),
			SeenAt: time.Now(),
		})
		c.cfg.Logger.Info("discovered solix device",
			"addr", a.Addr().String(),
			"name", a.LocalName(),
			"rssi", a.RSSI(),
		)
	}

	err := ble.Scan(scanCtx, false, handler, filter)
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		return found, fmt.Errorf("solix: scan: %w", err)
	}
	return found, nil
}

// Connect establishes a BLE connection to the Solix device at addr and
// performs the protocol negotiation. Returns a ready-to-use Device or an
// error.
//
// The Device automatically detects whether to use the encrypted (modern) or
// unencrypted (F2000 legacy) protocol based on the GATT services advertised
// by the device.
func (c *Client) Connect(ctx context.Context, addr string) (*Device, error) {
	connCtx, cancel := context.WithTimeout(ctx, c.cfg.ConnectTimeout)
	defer cancel()

	c.cfg.Logger.Info("connecting to solix device", "addr", addr)

	filter := func(a ble.Advertisement) bool {
		return a.Addr().String() == addr
	}

	conn, err := ble.Connect(connCtx, filter)
	if err != nil {
		return nil, fmt.Errorf("solix: connect to %s: %w", addr, err)
	}

	c.cfg.Logger.Info("BLE connection established", "addr", addr)

	// Discover GATT profile to determine protocol path
	profile, err := conn.DiscoverProfile(true)
	if err != nil {
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: discover profile on %s: %w", addr, err)
	}

	// Determine device name from connection metadata
	name := addr // fallback

	// Determine protocol path
	if hasChar(profile, protocol.UUIDTelemetry) {
		c.cfg.Logger.Info("using encrypted protocol path", "addr", addr)
		return c.connectEncrypted(ctx, conn, profile, addr, name)
	} else if hasChar(profile, uuidF2000Notify) {
		c.cfg.Logger.Info("using unencrypted F2000 protocol path", "addr", addr)
		return c.connectF2000(ctx, conn, profile, addr, name)
	}

	_ = conn.CancelConnection()
	return nil, fmt.Errorf("solix: %w: no known Solix GATT characteristics found on %s", ErrUnsupportedDevice, addr)
}

// connectEncrypted handles the ECDH-negotiated encrypted protocol path used by
// most modern Solix devices.
func (c *Client) connectEncrypted(ctx context.Context, conn ble.Client, profile *ble.Profile, addr, name string) (*Device, error) {
	cmdChar := findChar(profile, protocol.UUIDCommand)
	telChar := findChar(profile, protocol.UUIDTelemetry)
	if cmdChar == nil || telChar == nil {
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: required GATT characteristics missing on %s", addr)
	}

	devCtx, devCancel := context.WithCancel(ctx)

	dev := newDevice(addr, name, models.ModelUnknown)
	dev.cancelConn = devCancel

	enc := newEncryptedSession(conn, cmdChar, telChar, c.cfg.Logger)

	// Subscribe to telemetry notifications
	if err := conn.Subscribe(telChar, false, enc.onNotification); err != nil {
		devCancel()
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: subscribe to telemetry on %s: %w", addr, err)
	}

	// Run negotiation
	negCtx, negCancel := context.WithTimeout(ctx, c.cfg.NegotiationTimeout)
	defer negCancel()
	if err := enc.negotiate(negCtx); err != nil {
		devCancel()
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: negotiation with %s: %w", addr, err)
	}

	c.cfg.Logger.Info("encryption negotiation completed", "addr", addr)

	// Wire telemetry updates → Device
	enc.onTelemetry = func(params map[string][]byte) {
		s := buildStatusFromParams(params, models.ModelF2000)
		dev.updateStatus(s)
	}

	// Background connection watchdog
	go func() {
		defer dev.markDisconnected()
		select {
		case <-devCtx.Done():
		case <-conn.Disconnected():
		}
		c.cfg.Logger.Info("device disconnected", "addr", addr)
		_ = conn.CancelConnection()
	}()

	return dev, nil
}

// connectF2000 handles the unencrypted protocol path used by the
// F2000 / 767 PowerHouse on older firmware.
func (c *Client) connectF2000(ctx context.Context, conn ble.Client, profile *ble.Profile, addr, name string) (*Device, error) {
	writeChar := findChar(profile, uuidF2000Write)
	notifyChar := findChar(profile, uuidF2000Notify)
	if writeChar == nil || notifyChar == nil {
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: F2000 GATT characteristics missing on %s", addr)
	}

	devCtx, devCancel := context.WithCancel(ctx)
	dev := newDevice(addr, name, models.ModelF2000)
	dev.cancelConn = devCancel

	f := newF2000Session(conn, writeChar, notifyChar, c.cfg.Logger)
	f.onTelemetry = func(status models.F2000Status) {
		dev.updateStatus(f2000StatusToDeviceStatus(status))
	}

	if err := conn.Subscribe(notifyChar, false, f.onNotification); err != nil {
		devCancel()
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: subscribe F2000 on %s: %w", addr, err)
	}

	// Send initial telemetry query
	if err := conn.WriteCharacteristic(writeChar, f2000TelemetryQuery, false); err != nil {
		devCancel()
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: F2000 initial query on %s: %w", addr, err)
	}

	// Background watchdog
	go func() {
		defer dev.markDisconnected()
		select {
		case <-devCtx.Done():
		case <-conn.Disconnected():
		}
		c.cfg.Logger.Info("F2000 device disconnected", "addr", addr)
		_ = conn.CancelConnection()
	}()

	return dev, nil
}

// hasChar returns true if the profile contains a characteristic with uuid.
func hasChar(profile *ble.Profile, uuid string) bool {
	return findChar(profile, uuid) != nil
}

// findChar returns the characteristic from profile with the given UUID, or nil.
func findChar(profile *ble.Profile, uuid string) *ble.Characteristic {
	target := ble.MustParse(uuid)
	for _, svc := range profile.Services {
		for _, ch := range svc.Characteristics {
			if ch.UUID.Equal(target) {
				return ch
			}
		}
	}
	return nil
}

// buildStatusFromParams populates a DeviceStatus from a decoded TLV parameter
// map.  Currently maps F2000 params; other models can extend this.
func buildStatusFromParams(params map[string][]byte, m models.DeviceModel) DeviceStatus {
	f := &models.F2000{}
	_ = f.ParseTelemetry(params)
	s := f.Status()
	return f2000StatusToDeviceStatus(s)
}

func f2000StatusToDeviceStatus(s models.F2000Status) DeviceStatus {
	return DeviceStatus{
		Model:                     models.ModelF2000,
		BatteryPercent:            s.BatteryPercent,
		BatteryPercentExpansion:   s.BatteryPercentExpansion,
		BatteryHealth:             s.BatteryHealth,
		BatteryHealthExpansion:    s.BatteryHealthExpansion,
		NumExpansion:               s.NumExpansion,
		TimeRemainingHours:        s.TimeRemainingHours,
		DaysRemaining:             s.DaysRemaining,
		HoursRemaining:            s.HoursRemaining,
		TimestampRemaining:        s.TimestampRemaining,
		SolarPowerIn:              s.SolarPowerIn,
		ACPowerIn:                 s.ACPowerIn,
		ACPowerOut:                s.ACPowerOut,
		ACToBattery:               s.ACToBattery,
		ACPowerOutSockets:         s.ACPowerOutSockets,
		DC1PowerOut:               s.DC1PowerOut,
		DC2PowerOut:               s.DC2PowerOut,
		USBC1Power:                s.USBC1Power,
		USBC2Power:                s.USBC2Power,
		USBC3Power:                s.USBC3Power,
		USBA1Power:                s.USBA1Power,
		USBA2Power:                s.USBA2Power,
		Temperature:               s.Temperature,
		TemperatureExpansion:      s.TemperatureExpansion,
		SoftwareVersion:           s.SoftwareVersion,
		SoftwareVersionExpansion:  s.SoftwareVersionExpansion,
		SoftwareVersionController: s.SoftwareVersionController,
		SerialNumber:              s.SerialNumber,
		UpdatedAt:                 time.Now(),
	}
}

// uuidF2000Write is the write characteristic UUID for the unencrypted F2000 protocol.
const uuidF2000Write = "00007777-0000-1000-8000-00805f9b34fb"

// uuidF2000Notify is the notify characteristic UUID for the unencrypted F2000 protocol.
const uuidF2000Notify = "00008888-0000-1000-8000-00805f9b34fb"

// f2000TelemetryQuery is the fixed query frame to trigger a telemetry response.
var f2000TelemetryQuery = []byte{0x08, 0xEE, 0x00, 0x00, 0x00, 0x01, 0x01, 0x0A, 0x00, 0x02}

// encryptedSession manages the encrypted BLE session state for a single device.
type encryptedSession struct {
	conn        ble.Client
	cmdChar     *ble.Characteristic
	telChar     *ble.Characteristic
	log         *slog.Logger
	sharedSecret []byte
	negoState   int
	negoDone    chan struct{}
	fragBufs    map[string]map[int][]byte
	fragTotals  map[string]int
	onTelemetry func(params map[string][]byte)
	negoTS      time.Time
}

func newEncryptedSession(conn ble.Client, cmdChar, telChar *ble.Characteristic, log *slog.Logger) *encryptedSession {
	return &encryptedSession{
		conn:       conn,
		cmdChar:    cmdChar,
		telChar:    telChar,
		log:        log,
		negoState:  0,
		negoDone:   make(chan struct{}),
		fragBufs:   make(map[string]map[int][]byte),
		fragTotals: make(map[string]int),
	}
}

func (e *encryptedSession) negotiate(ctx context.Context) error {
	cmd0, err := protocol.NegotiationBytes(0)
	if err != nil {
		return err
	}
	if err := e.conn.WriteCharacteristic(e.cmdChar, cmd0, true); err != nil {
		return fmt.Errorf("send negotiation init: %w", err)
	}

	select {
	case <-e.negoDone:
		return nil
	case <-ctx.Done():
		return ErrNegotiationTimeout
	}
}

func (e *encryptedSession) onNotification(req []byte) {
	if len(req) < 9 {
		e.log.Warn("ignoring short packet", "len", len(req))
		return
	}

	pattern, cmd, payload, err := protocol.SplitPacket(req)
	if err != nil {
		e.log.Warn("packet parse error", "err", err)
		return
	}

	patHex := fmt.Sprintf("%x", pattern)
	cmdHex := fmt.Sprintf("%x", cmd)

	switch patHex {
	case "030001":
		e.handleNegotiation(cmdHex, payload)
	case "03010f", "030111":
		e.handleSession(cmdHex, payload, cmd)
	default:
		e.log.Debug("unknown pattern", "pattern", patHex)
	}
}

func (e *encryptedSession) handleNegotiation(cmdHex string, payload []byte) {
	e.log.Debug("negotiation message", "cmd", cmdHex)
	var stageReply int
	switch cmdHex {
	case "0801":
		stageReply = 1
	case "0803":
		stageReply = 2
	case "0829":
		e.negoTS = time.Now()
		stageReply = 3
	case "0805":
		stageReply = 4
	case "0821":
		// Extract device public key and derive shared secret
		params, err := protocol.ParsePayload(payload)
		if err != nil {
			e.log.Error("failed to parse stage 5 payload", "err", err)
			return
		}
		pubKeyBytes, ok := params["a1"]
		if !ok || len(pubKeyBytes) < 64 {
			e.log.Error("device public key missing or too short", "len", len(pubKeyBytes))
			return
		}
		secret, err := protocol.DeriveSharedSecret(pubKeyBytes)
		if err != nil {
			e.log.Error("ECDH failed", "err", err)
			return
		}
		e.sharedSecret = secret
		stageReply = 5
	case "4822":
		// Optional stage 6 — no response needed
		e.log.Debug("optional negotiation stage 6 received")
		return
	default:
		e.log.Warn("unexpected negotiation cmd", "cmd", cmdHex)
		return
	}

	b, err := protocol.NegotiationBytes(stageReply)
	if err != nil {
		e.log.Error("negotiation bytes error", "stage", stageReply, "err", err)
		return
	}
	if err := e.conn.WriteCharacteristic(e.cmdChar, b, false); err != nil {
		e.log.Error("write negotiation response failed", "stage", stageReply, "err", err)
		return
	}

	// If we just sent stage 5 reply, negotiation is complete
	if stageReply == 5 {
		close(e.negoDone)
	}
}

func (e *encryptedSession) handleSession(cmdHex string, payload []byte, cmd []byte) {
	if cmdHex == "0300" {
		// Non-encrypted telemetry
		params, err := protocol.ParsePayload(payload)
		if err == nil && e.onTelemetry != nil {
			e.onTelemetry(params)
		}
		return
	}

	if protocol.TelemetryCommands[cmdHex] {
		e.processTelemetryPacket(payload, cmdHex)
		return
	}

	// Unknown encrypted message — attempt decryption for debugging
	if e.sharedSecret != nil {
		e.log.Debug("unknown encrypted cmd", "cmd", cmdHex)
	}
}

func (e *encryptedSession) processTelemetryPacket(payload []byte, cmdKey string) {
	if len(payload) == 0 {
		return
	}
	fragIndex := int((payload[0] >> 4) & 0x0F)
	fragTotal := int(payload[0] & 0x0F)

	if fragTotal > 1 {
		fragData := payload[1:]
		if _, ok := e.fragBufs[cmdKey]; !ok || fragIndex == 1 {
			e.fragBufs[cmdKey] = make(map[int][]byte)
			e.fragTotals[cmdKey] = fragTotal
		}
		e.fragBufs[cmdKey][fragIndex] = fragData
		if len(e.fragBufs[cmdKey]) < fragTotal {
			return
		}
		// Reassemble
		var assembled []byte
		for i := 1; i <= fragTotal; i++ {
			assembled = append(assembled, e.fragBufs[cmdKey][i]...)
		}
		delete(e.fragBufs, cmdKey)
		delete(e.fragTotals, cmdKey)
		payload = assembled
	} else {
		payload = payload[1:]
	}

	if e.sharedSecret == nil {
		return
	}
	decrypted, err := protocol.DecryptPayload(e.sharedSecret, payload)
	if err != nil {
		e.log.Warn("decrypt failed", "err", err)
		return
	}
	params, err := protocol.ParsePayload(decrypted)
	if err != nil {
		e.log.Warn("payload parse failed", "err", err)
		return
	}
	if e.onTelemetry != nil {
		e.onTelemetry(params)
	}
}

// f2000Session manages the unencrypted F2000 BLE session.
type f2000Session struct {
	conn        ble.Client
	writeChar   *ble.Characteristic
	notifyChar  *ble.Characteristic
	log         *slog.Logger
	onTelemetry func(status models.F2000Status)
}

func newF2000Session(conn ble.Client, writeChar, notifyChar *ble.Characteristic, log *slog.Logger) *f2000Session {
	return &f2000Session{
		conn:       conn,
		writeChar:  writeChar,
		notifyChar: notifyChar,
		log:        log,
	}
}

// onNotification processes raw bytes from the F2000 unencrypted notify characteristic.
//
// Packet format:
//
//	byte[0:2]  header: 09 FF
//	byte[4]    00
//	byte[5]    packet_type (0x01=telemetry/state, 0x02=cmd)
//	byte[6]    sub_type (0x48=state ACK, 0x49=main telemetry, 0x01=aux)
//	byte[7:9]  length (little-endian)
//	...payload...
//	last byte  checksum (sum of all bytes & 0xFF)
func (f *f2000Session) onNotification(data []byte) {
	if len(data) < 8 {
		f.log.Warn("F2000 short notification", "len", len(data))
		return
	}

	// Validate checksum: sum of all bytes (including checksum) should be 0 mod 256
	var sum byte
	for _, b := range data {
		sum += b
	}
	if sum != 0 {
		f.log.Warn("F2000 checksum mismatch")
		return
	}

	subType := data[6]
	switch subType {
	case 0x49:
		// Main telemetry packet (102 bytes expected)
		if len(data) < 102 {
			f.log.Warn("F2000 telemetry packet too short", "len", len(data))
			return
		}
		s := parseF2000Telemetry(data)
		if f.onTelemetry != nil {
			f.onTelemetry(s)
		}
	case 0x48:
		f.log.Debug("F2000 state ACK packet received")
	case 0x01:
		f.log.Debug("F2000 auxiliary state packet received")
	default:
		f.log.Debug("F2000 unknown sub_type", "sub_type", fmt.Sprintf("%02x", subType))
	}
}

// parseF2000Telemetry decodes a 102-byte unencrypted F2000 telemetry packet.
func parseF2000Telemetry(data []byte) models.F2000Status {
	le16 := func(i int) int {
		if i+1 >= len(data) {
			return -1
		}
		return int(binary.LittleEndian.Uint16(data[i : i+2]))
	}

	s := models.F2000Status{
		TimeRemainingHours: float64(data[17]) / 10.0,
		DaysRemaining:      int(data[18]),

		ACPowerIn:    le16(19),
		ACPowerOut:   le16(21),
		USBC1Power:   le16(23),
		USBC2Power:   le16(25),
		USBC3Power:   le16(27),
		USBA1Power:   le16(29),
		USBA2Power:   le16(31),
		DC1PowerOut:  le16(33),
		DC2PowerOut:  le16(35),
		SolarPowerIn: le16(37),

		Temperature:          int(data[66]),
		TemperatureExpansion: int(data[67]),

		BatteryPercent:          int(data[70]),
		BatteryPercentExpansion: int(data[71]),
	}

	// Serial number from bytes 85:101
	if len(data) >= 101 {
		raw := data[85:101]
		end := len(raw)
		for end > 0 && raw[end-1] == 0 {
			end--
		}
		s.SerialNumber = string(raw[:end])
	}

	return s
}
