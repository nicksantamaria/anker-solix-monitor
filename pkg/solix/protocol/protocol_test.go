package protocol_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/nicksantamaria/anker-solix-monitor/pkg/solix/protocol"
)

// TestChecksum verifies the XOR checksum algorithm matches the Python reference.
func TestChecksum(t *testing.T) {
	tests := []struct {
		name     string
		input    string // hex
		expected byte
	}{
		{
			// negotiation command 0 sans last byte (b9)
			name:     "negotiation_cmd0_body",
			input:    "ff0936000300010001a10442ad8c69a22462326463306231372d623735642d346162662d626136652d656337633939376332336537",
			expected: 0xb9,
		},
		{
			// Simple known-good XOR: 0x01 ^ 0x02 ^ 0x03 = 0x00
			name:     "simple_xor",
			input:    "010203",
			expected: 0x00,
		},
		{
			// Single byte
			name:     "single_byte",
			input:    "ab",
			expected: 0xab,
		},
		{
			// Empty
			name:     "empty",
			input:    "",
			expected: 0x00,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := hex.DecodeString(tt.input)
			if err != nil {
				t.Fatalf("bad hex: %v", err)
			}
			got := protocol.Checksum(b)
			if got != tt.expected {
				t.Errorf("Checksum(%s) = %02x, want %02x", tt.input, got, tt.expected)
			}
		})
	}
}

// TestBuildPacketRoundtrip verifies that building a packet and then splitting
// it recovers the original pattern, cmd, and payload.
func TestBuildPacketRoundtrip(t *testing.T) {
	pattern := []byte{0x03, 0x01, 0x0F}
	cmd := []byte{0xC4, 0x02}
	payload := []byte{0x01, 0x02, 0x03, 0x04}

	pkt := protocol.BuildPacket(pattern, cmd, payload)

	gotPat, gotCmd, gotPayload, err := protocol.SplitPacket(pkt)
	if err != nil {
		t.Fatalf("SplitPacket: %v", err)
	}
	if !bytes.Equal(gotPat, pattern) {
		t.Errorf("pattern: got %x, want %x", gotPat, pattern)
	}
	if !bytes.Equal(gotCmd, cmd) {
		t.Errorf("cmd: got %x, want %x", gotCmd, cmd)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Errorf("payload: got %x, want %x", gotPayload, payload)
	}
}

// TestSplitPacket verifies validation errors for malformed packets.
func TestSplitPacket(t *testing.T) {
	t.Run("bad_header", func(t *testing.T) {
		pkt := []byte{0x00, 0x00, 0x09, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
		_, _, _, err := protocol.SplitPacket(pkt)
		if err == nil {
			t.Error("expected error for bad header")
		}
	})

	t.Run("length_mismatch", func(t *testing.T) {
		// Build a valid packet then corrupt the length field
		pkt := protocol.BuildPacket([]byte{0x03, 0x00, 0x01}, []byte{0x08, 0x01}, []byte{0xAA})
		pkt[2] = 0xFF // corrupt length
		_, _, _, err := protocol.SplitPacket(pkt)
		if err == nil {
			t.Error("expected error for length mismatch")
		}
	})

	t.Run("checksum_mismatch", func(t *testing.T) {
		pkt := protocol.BuildPacket([]byte{0x03, 0x00, 0x01}, []byte{0x08, 0x01}, []byte{0xAA})
		pkt[len(pkt)-1] ^= 0xFF // corrupt checksum
		_, _, _, err := protocol.SplitPacket(pkt)
		if err == nil {
			t.Error("expected error for checksum mismatch")
		}
	})

	t.Run("too_short", func(t *testing.T) {
		_, _, _, err := protocol.SplitPacket([]byte{0xFF, 0x09, 0x05})
		if err == nil {
			t.Error("expected error for too-short packet")
		}
	})
}

// TestNegotiationPacketChecksums verifies the checksum of all fixed negotiation
// command bytes matches the last byte in each command (the reference
// implementation validates these at runtime).
func TestNegotiationPacketChecksums(t *testing.T) {
	for stage := 0; stage <= 5; stage++ {
		t.Run(string(rune('0'+stage)), func(t *testing.T) {
			b, err := protocol.NegotiationBytes(stage)
			if err != nil {
				t.Fatalf("NegotiationBytes(%d): %v", stage, err)
			}
			// Validate via SplitPacket which checks all invariants
			_, _, _, err = protocol.SplitPacket(b)
			if err != nil {
				t.Errorf("NegotiationBytes(%d) failed SplitPacket: %v", stage, err)
			}
		})
	}
}

// TestBuildPacketHeader verifies the packet always starts with FF 09.
func TestBuildPacketHeader(t *testing.T) {
	pkt := protocol.BuildPacket([]byte{0x03, 0x00, 0x01}, []byte{0x00, 0x01}, nil)
	if pkt[0] != 0xFF || pkt[1] != 0x09 {
		t.Errorf("packet header = %02x%02x, want ff09", pkt[0], pkt[1])
	}
}

// TestBuildPacketLength verifies the encoded length field is correct.
func TestBuildPacketLength(t *testing.T) {
	payload := make([]byte, 16)
	pkt := protocol.BuildPacket([]byte{0x03, 0x01, 0x0F}, []byte{0xC4, 0x02}, payload)
	// expected: 2(header)+2(len)+3(pattern)+2(cmd)+16(payload)+1(checksum) = 26
	expected := 26
	if len(pkt) != expected {
		t.Errorf("len(pkt) = %d, want %d", len(pkt), expected)
	}
	encodedLen := int(pkt[2]) | int(pkt[3])<<8
	if encodedLen != expected {
		t.Errorf("encoded length = %d, want %d", encodedLen, expected)
	}
}

// TestParsePayload verifies TLV payload parsing.
func TestParsePayload(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		// param 0xa5 length=3 data=[01 e8 03]
		raw := []byte{0xa5, 0x03, 0x01, 0xe8, 0x03}
		params, err := protocol.ParsePayload(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(params) != 1 {
			t.Errorf("expected 1 param, got %d", len(params))
		}
		v, ok := params["a5"]
		if !ok {
			t.Fatal("missing param a5")
		}
		if !bytes.Equal(v, []byte{0x01, 0xe8, 0x03}) {
			t.Errorf("param a5 = %x, want 01e803", v)
		}
	})

	t.Run("leading_zero_stripped", func(t *testing.T) {
		// payload starts with 0x00 which should be stripped
		raw := []byte{0x00, 0xc1, 0x02, 0x01, 0x64}
		params, err := protocol.ParsePayload(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := params["c1"]; !ok {
			t.Error("expected param c1")
		}
	})

	t.Run("multiple_params", func(t *testing.T) {
		// two params: a5=[01,00] and a6=[02,00]
		raw := []byte{
			0xa5, 0x02, 0x01, 0x00,
			0xa6, 0x02, 0x02, 0x00,
		}
		params, err := protocol.ParsePayload(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(params) != 2 {
			t.Errorf("expected 2 params, got %d", len(params))
		}
	})

	t.Run("empty_payload", func(t *testing.T) {
		params, err := protocol.ParsePayload([]byte{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(params) != 0 {
			t.Errorf("expected 0 params, got %d", len(params))
		}
	})
}

// TestBuildCommandPayload verifies the timestamp suffix is appended correctly.
func TestBuildCommandPayload(t *testing.T) {
	payload := []byte{0xa1, 0x01, 0x21}
	result := protocol.BuildCommandPayload(payload, 0)

	// Should be original payload + fe 05 03 + 4 timestamp bytes = 10 bytes total
	if len(result) != len(payload)+7 {
		t.Errorf("result length = %d, want %d", len(result), len(payload)+7)
	}
	if result[3] != 0xFE || result[4] != 0x05 || result[5] != 0x03 {
		t.Errorf("timestamp suffix not found: %x", result[3:6])
	}
}

// TestEncryptDecryptRoundtrip verifies that encrypting then decrypting yields
// the original plaintext.
func TestEncryptDecryptRoundtrip(t *testing.T) {
	// 32-byte shared secret: first 16 = AES key, last 16 = IV
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}

	plaintext := []byte("hello solix world!")

	ciphertext, err := protocol.EncryptPayload(secret, plaintext)
	if err != nil {
		t.Fatalf("EncryptPayload: %v", err)
	}

	recovered, err := protocol.DecryptPayload(secret, ciphertext)
	if err != nil {
		t.Fatalf("DecryptPayload: %v", err)
	}

	if !bytes.Equal(recovered, plaintext) {
		t.Errorf("roundtrip failed: got %q, want %q", recovered, plaintext)
	}
}

// TestDecryptPayloadBlockSizeError verifies that a non-block-aligned ciphertext
// returns an error.
func TestDecryptPayloadBlockSizeError(t *testing.T) {
	secret := make([]byte, 32)
	_, err := protocol.DecryptPayload(secret, []byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Error("expected error for non-block-aligned ciphertext")
	}
}
