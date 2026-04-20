package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/zalando/go-keyring"
)

const (
	dbKeyringService = "phaze-db-vault"
	dbKeyringAccount = "default"
)

func dbVaultKey() ([]byte, error) {
	s, err := keyring.Get(dbKeyringService, dbKeyringAccount)
	if err == nil && s != "" {
		k, err := base64.StdEncoding.DecodeString(s)
		if err == nil && len(k) == 32 {
			return k, nil
		}
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := keyring.Set(dbKeyringService, dbKeyringAccount, base64.StdEncoding.EncodeToString(k)); err != nil {
		return nil, err
	}
	return k, nil
}

func decryptDBFile(path string) error {
	enc := path + ".enc"
	st, err := os.Stat(enc)
	if err != nil || st.Size() == 0 {
		return nil
	}
	key, err := dbVaultKey()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(enc)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return fmt.Errorf("vault file truncated")
	}
	nonce, ct := data[:ns], data[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, pt, 0600); err != nil {
		return err
	}
	return nil
}

func encryptDBFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	key, err := dbVaultKey()
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nonce, nonce, data, nil)
	if err := os.WriteFile(path+".enc", ct, 0600); err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}
