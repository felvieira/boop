// Package secretbox provides authenticated encryption for small persistent secrets.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const keySize = 32

type Box struct {
	aead cipher.AEAD
}

// OpenOrCreate loads a 256-bit key, creating a private key file on first run.
func OpenOrCreate(path string) (*Box, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("create secret key directory: %w", err)
	}
	key, created, err := readOrCreateKey(path)
	if err != nil {
		return nil, created, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, created, fmt.Errorf("initialize secret encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, created, fmt.Errorf("initialize secret encryption: %w", err)
	}
	return &Box{aead: aead}, created, nil
}

func readOrCreateKey(path string) ([]byte, bool, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != keySize {
			return nil, false, fmt.Errorf("secret encryption key file has invalid length")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, false, fmt.Errorf("secure secret encryption key file: %w", err)
		}
		return key, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("read secret encryption key file: %w", err)
	}

	key = make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, false, fmt.Errorf("generate secret encryption key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		key, err = os.ReadFile(path)
		if err != nil {
			return nil, false, fmt.Errorf("read concurrent secret encryption key file: %w", err)
		}
		if len(key) != keySize {
			return nil, false, fmt.Errorf("secret encryption key file has invalid length")
		}
		return key, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("create secret encryption key file: %w", err)
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		os.Remove(path)
		return nil, false, fmt.Errorf("write secret encryption key file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return nil, false, fmt.Errorf("sync secret encryption key file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, false, fmt.Errorf("close secret encryption key file: %w", err)
	}
	return key, true, nil
}

// Encrypt returns a versioned base64 encoding of a nonce and authenticated ciphertext.
func (b *Box) Encrypt(plaintext, associatedData []byte) (string, error) {
	if b == nil || b.aead == nil {
		return "", fmt.Errorf("secret encryption is unavailable")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret encryption nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, plaintext, associatedData)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt.
func (b *Box) Decrypt(encoded string, associatedData []byte) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, fmt.Errorf("secret encryption is unavailable")
	}
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted setting: %w", err)
	}
	if len(sealed) < b.aead.NonceSize()+b.aead.Overhead() {
		return nil, fmt.Errorf("encrypted setting is truncated")
	}
	nonce, ciphertext := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return nil, fmt.Errorf("decrypt setting: %w", err)
	}
	return plaintext, nil
}
