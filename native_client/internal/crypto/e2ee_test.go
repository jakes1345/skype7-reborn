package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	if kp.Public == nil || kp.Private == nil {
		t.Fatal("KeyPair contains nil keys")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()

	plaintext := []byte("Hello, Phaze!")

	encrypted, err := Encrypt(plaintext, kp2.Public, kp1.Private)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, kp1.Public, kp2.Private)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("Decrypted != plaintext: %q vs %q", decrypted, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()
	kp3, _ := GenerateKeyPair()

	plaintext := []byte("Secret message")
	encrypted, _ := Encrypt(plaintext, kp2.Public, kp1.Private)

	_, err := Decrypt(encrypted, kp3.Public, kp2.Private)
	if err == nil {
		t.Fatal("Expected decryption to fail with wrong key")
	}
}

func TestFingerprint(t *testing.T) {
	kp, _ := GenerateKeyPair()

	fp1 := Fingerprint(kp.Public)
	fp2 := Fingerprint(kp.Public)

	if fp1 != fp2 {
		t.Fatal("Same key should produce same fingerprint")
	}

	kp2, _ := GenerateKeyPair()
	fp3 := Fingerprint(kp2.Public)

	if fp1 == fp3 {
		t.Fatal("Different keys should produce different fingerprints")
	}
}

func TestFingerprintLength(t *testing.T) {
	kp, _ := GenerateKeyPair()
	fp := Fingerprint(kp.Public)

	if len(fp) != 16 {
		t.Fatalf("Fingerprint should be 16 hex chars (8 bytes), got %d", len(fp))
	}
}
