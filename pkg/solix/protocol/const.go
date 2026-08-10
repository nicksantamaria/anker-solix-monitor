// Package protocol implements the Anker Solix BLE wire protocol.
//
// The protocol uses a custom binary framing over BLE GATT characteristics,
// with ECDH key exchange followed by AES-128-CBC encryption for all
// subsequent telemetry and command packets.
//
// Packet format:
//
//	<HEADER 2B> <LENGTH 2B> <PATTERN 3B> <CMD 2B> <PAYLOAD nB> <CHECKSUM 1B>
//
// The header is always 0xFF 0x09. Length is little-endian and includes all
// bytes in the packet. The checksum is the XOR of all preceding bytes.
package protocol

// UUIDs for GATT characteristics used by Solix devices.
const (
	// UUIDTelemetry is the GATT characteristic UUID for subscribing to
	// telemetry notifications from the device. Handle 17.
	UUIDTelemetry = "8c850003-0302-41c5-b46e-cf057c562025"

	// UUIDCommand is the GATT characteristic UUID for writing commands and
	// performing the ECDH negotiation.
	UUIDCommand = "8c850002-0302-41c5-b46e-cf057c562025"

	// UUIDIdentifier is the GATT service UUID that is advertised by Solix and
	// Prime devices, used for discovery filtering.
	UUIDIdentifier = "0000ff09-0000-1000-8000-00805f9b34fb"
)

// Packet header bytes that prefix every Solix BLE packet.
var packetHeader = []byte{0xFF, 0x09}

// patternNegotiation is the 3-byte pattern for negotiation messages.
var patternNegotiation = []byte{0x03, 0x00, 0x01}

// patternSessionA and patternSessionB are the two 3-byte patterns seen in
// session (post-negotiation) messages.
var patternSessionA = []byte{0x03, 0x01, 0x0F}
var patternSessionB = []byte{0x03, 0x01, 0x11}

// patternEncryptedSend is the pattern used when sending encrypted commands.
var patternEncryptedSend = []byte{0x03, 0x00, 0x0F}

// TelemetryCommands are the command codes that carry telemetry for most
// devices. The F2000 uses c402/c405; 4300 is also common.
var TelemetryCommands = map[string]bool{
	"c402": true,
	"4300": true,
	"c405": true,
}

// Negotiation command bytes as defined in the Python reference implementation.
// These are fixed payloads that implement the ECDH handshake protocol.
const (
	// negotiationCmd0 initiates the handshake.
	negotiationCmd0 = "ff0936000300010001a10442ad8c69a22462326463306231372d623735642d346162662d626136652d656337633939376332336537b9"

	// negotiationCmd1 responds to stage 1 (cmd 0801).
	negotiationCmd1 = "ff093d000300010003a10442ad8c69a22462326463306231372d623735642d346162662d626136652d656337633939376332336537a30120a40200f064"

	// negotiationCmd2 responds to stage 2 (cmd 0803).
	negotiationCmd2 = "ff0936000300010029a10442ad8c69a22462326463306231372d623735642d346162662d626136652d65633763393937633233653791"

	// negotiationCmd3 responds to stage 3 (cmd 0829).
	negotiationCmd3 = "ff0940000300010005a10443ad8c69a22462326463306231372d623735642d346162662d626136652d656337633939376332336537a30120a40200f0a50140fa"

	// negotiationCmd4 responds to stage 4 (cmd 0805) and contains the ECDH public key.
	negotiationCmd4 = "ff094c000300010021a140060ea168f232aedb37fb2d120c49180329ac72ab5ec3eb8fd30a2f252dc5e151dabccd9b1dc1e288704ca760a0d8c918e5c94823a1f609a4bf07fb4c33ee219085"

	// negotiationCmd5 responds to stage 5 (cmd 0821).
	negotiationCmd5 = "ff095a000300014022580bc0532a53c739adf3da7b994a7b5f221bcc16bab6392c215cb4faaf41d9d58e2c81c016e474c78eed5569147cb74a1f22ca2b3fad2e209dbbcfbdaca352034a6c479f055f68581b5f1e22348809f526"

	// baseTimestamp is the agreed-upon unix timestamp used for anti-replay
	// protection in commands (little-endian hex).
	baseTimestamp = "42ad8c69"

	// privateKey is the ECDH private key used to negotiate the shared secret.
	// This is a known-fixed key from the SolixBLE Python reference implementation.
	privateKey = "7dfbea61cd95cee49c458ad7419e817f1ade9a66136de3c7d5787af1458e39f4"
)
