package crypto

import (
	"crypto/rand"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/box"
)

// KeyPair represents a sovereign crypto identity
type KeyPair struct {
	Public  *[32]byte
	Private *[32]byte
}

// GenerateKeyPair creates a new forensic-grade key pair
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyPair{Public: pub, Private: priv}, nil
}

// Encrypt wraps a message for a specific recipient
func Encrypt(msg []byte, recipientPub *[32]byte, senderPriv *[32]byte) ([]byte, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	// Seal the message
	out := box.Seal(nonce[:], msg, &nonce, recipientPub, senderPriv)
	return out, nil
}

// Decrypt unwraps a message from a sender
func Decrypt(encrypted []byte, senderPub *[32]byte, recipientPriv *[32]byte) ([]byte, error) {
	if len(encrypted) < 24 {
		return nil, fmt.Errorf("message too short")
	}

	var nonce [24]byte
	copy(nonce[:], encrypted[:24])
	
	msg, ok := box.Open(nil, encrypted[24:], &nonce, senderPub, recipientPriv)
	if !ok {
		return nil, fmt.Errorf("decryption failed: invalid key or tampered data")
	}
	return msg, nil
}
