package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/elliptic"
	"encoding/binary"
	"fmt"
	"math/big"
)

// DeriveSharedSecret performs ECDH on P-256 using the hardcoded private key
// and the device's uncompressed public key bytes.
//
// devicePubKeyBytes should be the 64 raw bytes of the public key (without the
// 0x04 uncompressed-point prefix). The 0x04 prefix is prepended internally.
//
// The returned 32-byte shared secret is used as: first 16 bytes = AES key,
// last 16 bytes = IV.
func DeriveSharedSecret(devicePubKeyBytes []byte) ([]byte, error) {
	privKeyBytes := PrivateKeyBytes()
	privKey := new(big.Int).SetBytes(privKeyBytes)

	curve := elliptic.P256()

	// Prepend 0x04 uncompressed-point prefix if the caller omitted it
	var fullKey []byte
	if len(devicePubKeyBytes) == 64 {
		fullKey = append([]byte{0x04}, devicePubKeyBytes...)
	} else {
		fullKey = devicePubKeyBytes
	}

	x, y := elliptic.Unmarshal(curve, fullKey)
	if x == nil {
		return nil, fmt.Errorf("failed to unmarshal device public key")
	}

	// Compute shared point: privKey * (x, y)
	sharedX, _ := curve.ScalarMult(x, y, privKey.Bytes())
	if sharedX == nil {
		return nil, fmt.Errorf("ECDH scalar multiplication failed")
	}

	// Return shared X coordinate zero-padded to 32 bytes
	secret := make([]byte, 32)
	sharedX.FillBytes(secret)
	return secret, nil
}

// EncryptPayload encrypts payload bytes using AES-128-CBC with PKCS7 padding.
// sharedSecret[0:16] is the key, sharedSecret[16:32] is the IV.
func EncryptPayload(sharedSecret, payload []byte) ([]byte, error) {
	key := sharedSecret[:16]
	iv := sharedSecret[16:32]

	padded := pkcs7Pad(payload, aes.BlockSize)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, padded)
	return encrypted, nil
}

// DecryptPayload decrypts an AES-128-CBC encrypted payload.
func DecryptPayload(sharedSecret, payload []byte) ([]byte, error) {
	if len(payload)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of block size", len(payload))
	}
	key := sharedSecret[:16]
	iv := sharedSecret[16:32]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	decrypted := make([]byte, len(payload))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(decrypted, payload)
	return pkcs7Unpad(decrypted)
}

// BuildCommandPayload appends the timestamp suffix required by the anti-replay
// mechanism. elapsed is the number of seconds since negotiation completed.
func BuildCommandPayload(payload []byte, elapsed uint32) []byte {
	base := BaseTimestampBytes()
	baseTS := binary.LittleEndian.Uint32(base)
	ts := baseTS + elapsed

	tsBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(tsBytes, ts)

	suffix := append([]byte{0xFE, 0x05, 0x03}, tsBytes...)
	result := make([]byte, len(payload)+len(suffix))
	copy(result, payload)
	copy(result[len(payload):], suffix)
	return result
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize {
		return nil, fmt.Errorf("invalid PKCS7 padding value: %d", padding)
	}
	if len(data) < padding {
		return nil, fmt.Errorf("data shorter than padding")
	}
	return data[:len(data)-padding], nil
}
