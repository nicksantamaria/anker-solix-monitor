package solix

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-ble/ble"
	solixble "github.com/nicksantamaria/anker-solix-monitor/pkg/solix/ble"
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

// NewClient initialises the BLE stack and returns a ready-to-use
// Client.
func NewClient(cfg Config) (*Client, error) {
	cfg.setDefaults()

	d, err := solixble.NewDevice()
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
		c.cfg.Logger.Debug("scanning device", "name", a.LocalName(), "addr", a.Addr().String(), "rssi", a.RSSI())
		if a.LocalName() == "767_PowerHouse" {
			return true
		}
		for _, svcUUID := range a.Services() {
			c.cfg.Logger.Debug("advertised service", "name", a.LocalName(), "uuid", svcUUID.String())
			if svcUUID.Equal(ble.MustParse(protocol.UUIDIdentifier)) || svcUUID.Equal(ble.MustParse("ff09")) {
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

	// Use a channel to capture the result of ble.Dial
	type result struct {
		conn ble.Client
		err  error
	}
	ch := make(chan result, 1)

	c.cfg.Logger.Debug("calling ble.Dial", "addr", addr)
	go func() {
		conn, err := ble.Dial(connCtx, ble.NewAddr(addr))
		ch <- result{conn, err}
	}()

	var conn ble.Client
	select {
	case res := <-ch:
		if res.err != nil {
			c.cfg.Logger.Debug("ble.Dial failed", "addr", addr, "err", res.err)
			return nil, fmt.Errorf("solix: connect to %s: %w", addr, res.err)
		}
		conn = res.conn
	case <-connCtx.Done():
		c.cfg.Logger.Debug("ble.Dial timed out or was canceled", "addr", addr, "err", connCtx.Err())
		return nil, fmt.Errorf("solix: connect to %s: %w", addr, connCtx.Err())
	}

	c.cfg.Logger.Info("BLE connection established", "addr", addr)

	c.cfg.Logger.Debug("discovering GATT profile", "addr", addr)
	type profileResult struct {
		profile *ble.Profile
		err     error
	}
	pch := make(chan profileResult, 1)
	go func() {
		p, err := conn.DiscoverProfile(true)
		pch <- profileResult{p, err}
	}()

	var profile *ble.Profile
	select {
	case res := <-pch:
		if res.err != nil {
			c.cfg.Logger.Debug("DiscoverProfile failed", "addr", addr, "err", res.err)
			_ = conn.CancelConnection()
			return nil, fmt.Errorf("solix: discover profile on %s: %w", addr, res.err)
		}
		profile = res.profile
	case <-connCtx.Done():
		c.cfg.Logger.Debug("DiscoverProfile timed out", "addr", addr)
		_ = conn.CancelConnection()
		return nil, fmt.Errorf("solix: discover profile on %s: %w", addr, connCtx.Err())
	}
	c.cfg.Logger.Debug("GATT profile discovered", "addr", addr)

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
	c.cfg.Logger.Debug("starting encrypted protocol negotiation", "addr", addr)
	if err := enc.negotiate(negCtx); err != nil {
		c.cfg.Logger.Debug("negotiation failed", "addr", addr, "err", err)
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

	c.cfg.Logger.Debug("starting F2000 initial telemetry query", "addr", addr)
	if err := conn.WriteCharacteristic(writeChar, f2000TelemetryQuery, true); err != nil {
		c.cfg.Logger.Debug("F2000 initial query failed (noRsp=true)", "addr", addr, "err", err)
		// Fallback to write with response if without response is not supported
		if err := conn.WriteCharacteristic(writeChar, f2000TelemetryQuery, false); err != nil {
			c.cfg.Logger.Debug("F2000 initial query failed (noRsp=false)", "addr", addr, "err", err)
			devCancel()
			_ = conn.CancelConnection()
			return nil, fmt.Errorf("solix: F2000 initial query on %s: %w", addr, err)
		}
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
	var shortTarget ble.UUID
	if len(uuid) == 36 && (uuid[8:] == "-0000-1000-8000-00805f9b34fb") {
		shortTarget = ble.MustParse(uuid[4:8])
	}

	for _, svc := range profile.Services {
		for _, ch := range svc.Characteristics {
			if ch.UUID.Equal(target) || (shortTarget != nil && ch.UUID.Equal(shortTarget)) {
				// Log characteristic properties for debugging if writing fails
				slog.Debug("found characteristic", "uuid", ch.UUID.String(), "props", fmt.Sprintf("%02x", ch.Property))
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
		NumExpansion:              s.NumExpansion,
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
	conn         ble.Client
	cmdChar      *ble.Characteristic
	telChar      *ble.Characteristic
	log          *slog.Logger
	sharedSecret []byte
	negoState    int
	negoDone     chan struct{}
	fragBufs     map[string]map[int][]byte
	fragTotals   map[string]int
	onTelemetry  func(params map[string][]byte)
	negoTS       time.Time
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
		e.log.Debug("send negotiation init failed (noRsp=true)", "err", err)
		if err := e.conn.WriteCharacteristic(e.cmdChar, cmd0, false); err != nil {
			e.log.Debug("send negotiation init failed (noRsp=false)", "err", err)
			return fmt.Errorf("send negotiation init: %w", err)
		}
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
	if err := e.conn.WriteCharacteristic(e.cmdChar, b, true); err != nil {
		e.log.Debug("write negotiation response failed (noRsp=true)", "stage", stageReply, "err", err)
		if err := e.conn.WriteCharacteristic(e.cmdChar, b, false); err != nil {
			e.log.Error("write negotiation response failed (noRsp=false)", "stage", stageReply, "err", err)
			return
		}
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

	// Validate checksum: XOR of all bytes should be 0, OR sum of preceding bytes should equal last byte
	var xorSum byte
	for _, b := range data {
		xorSum ^= b
	}

	if xorSum != 0 {
		var addSum byte
		for i := 0; i < len(data)-1; i++ {
			addSum += data[i]
		}
		if addSum != data[len(data)-1] {
			f.log.Warn("F2000 checksum mismatch", "xor", fmt.Sprintf("%02x", xorSum), "add", fmt.Sprintf("%02x", addSum), "expected", fmt.Sprintf("%02x", data[len(data)-1]), "raw", fmt.Sprintf("%x", data))
			return
		}
	}

	subType := data[6]
	switch subType {
	case 0x49, 0x01:
		// Telemetry packet (0x49 is 102 bytes, 0x01 is 122 bytes)
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
	default:
		f.log.Debug("F2000 unknown sub_type", "sub_type", fmt.Sprintf("%02x", subType))
	}
}

// parseF2000Telemetry decodes an unencrypted F2000 telemetry packet.
// Supports both the 102-byte (0x49) and 122-byte (0x01) variants.
func parseF2000Telemetry(data []byte) models.F2000Status {
	le16 := func(i int) int {
		if i+1 >= len(data) {
			return -1
		}
		return int(binary.LittleEndian.Uint16(data[i : i+2]))
	}

	s := models.F2000Status{}

	// Common fields for both 0x49 and 0x01 subtypes
	s.TimeRemainingHours = float64(data[17]) / 10.0
	s.DaysRemaining = int(data[18])
	s.ACPowerIn = le16(19)
	s.ACPowerOut = le16(21)
	s.USBC1Power = le16(23)
	s.USBC2Power = le16(25)
	s.USBC3Power = le16(27)
	s.USBA1Power = le16(29)
	s.USBA2Power = le16(31)
	s.DC1PowerOut = le16(33)
	s.DC2PowerOut = le16(35)
	s.SolarPowerIn = le16(37)
	s.Temperature = int(data[66])
	s.TemperatureExpansion = int(data[67])
	s.BatteryPercent = int(data[70])
	s.BatteryPercentExpansion = int(data[71])
	s.BatteryHealth = int(data[72])
	s.NumExpansion = int(data[80])

	// Serial number from bytes 85:101 for both variants
	if len(data) >= 101 {
		raw := data[85:101]
		end := len(raw)
		for end > 0 && raw[end-1] == 0 {
			end--
		}
		s.SerialNumber = string(raw[:end])
	}

	if s.TimeRemainingHours > 0 {
		days := int(s.TimeRemainingHours) / 24
		hours := s.TimeRemainingHours - float64(days*24)
		s.DaysRemaining = days
		s.HoursRemaining = float64(int(hours*10+0.5)) / 10.0
		ts := time.Now().Add(time.Duration(s.TimeRemainingHours * float64(time.Hour)))
		s.TimestampRemaining = &ts
	}

	return s
}
