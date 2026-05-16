package ui

import "testing"

func TestReadAssetRawRejectsUnsafePath(t *testing.T) {
	if _, ok := ReadAssetRaw("../etc/passwd"); ok {
		t.Fatal("expected false for path traversal")
	}
}

func TestVaultSoundBytesRejectsUnsafeName(t *testing.T) {
	if _, ok := VaultSoundBytes("../etc/passwd"); ok {
		t.Fatal("expected false for path traversal")
	}
	if _, ok := VaultSoundBytes("a/b.wav"); ok {
		t.Fatal("expected false for slash in name")
	}
}
