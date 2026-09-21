package protocol

import (
	"encoding/hex"
	"fmt"
)

// BuildPayloadOnOff returns the TLV payload for a binary on/off command.
//
// On:  a1 01 21 a2 02 01 01
// Off: a1 01 21 a2 02 01 00
func BuildPayloadOnOff(on bool) []byte {
	v := byte(0x00)
	if on {
		v = 0x01
	}
	return BuildPayloadLevel(v)
}

// BuildPayloadLevel returns the TLV payload for a brightness or mode level
// command (e.g. display brightness 0–3, LED mode 0–4).
//
// a1 01 21 a2 02 01 <level>
func BuildPayloadLevel(level byte) []byte {
	return []byte{0xa1, 0x01, 0x21, 0xa2, 0x02, 0x01, level}
}

// BuildPayloadUint16 returns the TLV payload carrying a 16-bit little-endian
// value. Used for timeout and AC charging power commands.
//
// a1 01 21 a2 03 02 <lo> <hi>
func BuildPayloadUint16(value uint16) []byte {
	return []byte{0xa1, 0x01, 0x21, 0xa2, 0x03, 0x02, byte(value), byte(value >> 8)}
}


// The checksum byte is appended to the end of the packet.
func Checksum(packet []byte) byte {
	var v byte
	for _, b := range packet {
		v ^= b
	}
	return v
}

// BuildPacket constructs a framed BLE packet ready for transmission.
//
// Format: FF09 <len LE2> <pattern 3B> <cmd 2B> <payload nB> <checksum 1B>
func BuildPacket(pattern, cmd, payload []byte) []byte {
	totalLen := 2 + 2 + 3 + 2 + len(payload) + 1
	lenBytes := []byte{byte(totalLen), byte(totalLen >> 8)}

	pkt := make([]byte, 0, totalLen)
	pkt = append(pkt, packetHeader...)
	pkt = append(pkt, lenBytes...)
	pkt = append(pkt, pattern...)
	pkt = append(pkt, cmd...)
	pkt = append(pkt, payload...)
	pkt = append(pkt, Checksum(pkt))
	return pkt
}

// SplitPacket validates and decomposes a raw BLE notification into its
// constituent parts: pattern, cmd, and payload.
func SplitPacket(packet []byte) (pattern, cmd, payload []byte, err error) {
	if len(packet) < 9 {
		return nil, nil, nil, fmt.Errorf("packet too short: %d bytes", len(packet))
	}

	if packet[0] != 0xFF || packet[1] != 0x09 {
		return nil, nil, nil, fmt.Errorf("packet does not start with FF09: %02x%02x", packet[0], packet[1])
	}

	encodedLen := int(packet[2]) | int(packet[3])<<8
	if encodedLen != len(packet) {
		return nil, nil, nil, fmt.Errorf("packet length encoded as %d but actual length is %d", encodedLen, len(packet))
	}

	expectedChecksum := Checksum(packet[:len(packet)-1])
	if packet[len(packet)-1] != expectedChecksum {
		return nil, nil, nil, fmt.Errorf("checksum mismatch: got %02x, expected %02x", packet[len(packet)-1], expectedChecksum)
	}

	pattern = packet[4:7]
	cmd = packet[7:9]
	payload = packet[9 : len(packet)-1]
	return pattern, cmd, payload, nil
}

// ParsePayload parses the binary payload format into a map of parameter
// id → raw bytes.
//
// Payload format: (<id 1B> <len 1B> <data nB>)*
// Payloads may optionally begin with a 0x00 byte which is stripped.
func ParsePayload(payload []byte) (map[string][]byte, error) {
	result := make(map[string][]byte)
	data := make([]byte, len(payload))
	copy(data, payload)

	pos := 0

	// Strip optional leading 0x00
	if len(data) > 0 && data[0] == 0x00 {
		pos++
	}

	for pos < len(data) {
		if pos >= len(data) {
			break
		}
		paramID := fmt.Sprintf("%02x", data[pos])
		pos++

		// A param_id at the end with no length/data
		if pos >= len(data) {
			result[paramID] = []byte{}
			break
		}

		paramLen := int(data[pos])
		pos++

		if pos+paramLen > len(data) {
			return result, fmt.Errorf("param %s: expected %d bytes but only %d remain", paramID, paramLen, len(data)-pos)
		}

		paramData := make([]byte, paramLen)
		copy(paramData, data[pos:pos+paramLen])
		result[paramID] = paramData
		pos += paramLen
	}

	return result, nil
}

// NegotiationBytes returns the fixed command bytes for each negotiation stage.
func NegotiationBytes(stage int) ([]byte, error) {
	var s string
	switch stage {
	case 0:
		s = negotiationCmd0
	case 1:
		s = negotiationCmd1
	case 2:
		s = negotiationCmd2
	case 3:
		s = negotiationCmd3
	case 4:
		s = negotiationCmd4
	case 5:
		s = negotiationCmd5
	default:
		return nil, fmt.Errorf("unknown negotiation stage: %d", stage)
	}
	return hex.DecodeString(s)
}

// BaseTimestampBytes returns the base timestamp as little-endian bytes.
func BaseTimestampBytes() []byte {
	b, _ := hex.DecodeString(baseTimestamp)
	return b
}

// PrivateKeyBytes returns the ECDH private key bytes.
func PrivateKeyBytes() []byte {
	b, _ := hex.DecodeString(privateKey)
	return b
}

// EncryptedSendPattern returns the pattern used for sending encrypted commands.
func EncryptedSendPattern() []byte {
	return append([]byte{}, patternEncryptedSend...)
}
