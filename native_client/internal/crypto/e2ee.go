package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/box"
)

type KeyPair struct {
	Public  *[32]byte
	Private *[32]byte
}

func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyPair{Public: pub, Private: priv}, nil
}

func Encrypt(msg []byte, recipientPub *[32]byte, senderPriv *[32]byte) ([]byte, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	out := box.Seal(nonce[:], msg, &nonce, recipientPub, senderPriv)
	return out, nil
}

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

func Fingerprint(pub *[32]byte) string {
	hash := sha256.Sum256(pub[:])
	return hex.EncodeToString(hash[:8])
}

func VerifyKey(theirPub, myPriv *[32]byte, testMsg []byte) (bool, error) {
	encrypted, err := Encrypt(testMsg, theirPub, myPriv)
	if err != nil {
		return false, err
	}
	decrypted, err := Decrypt(encrypted, theirPub, myPriv)
	if err != nil {
		return false, err
	}
	return string(decrypted) == string(testMsg), nil
}
