package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"io"
)

// Encrypt secures a plaintext payload using AES-GCM.
// It zeroes out the plaintext buffer after encryption to ensure memory hygiene.
func Encrypt(plaintext []byte, hexKey string) (ciphertext []byte, nonce []byte, err error) {
	// Ensure memory is wiped even if a panic occurs
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()

	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	nonce = make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}

	ciphertext = aesGCM.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Decrypt restores the plaintext from an AES-GCM ciphertext and nonce.
// The caller is responsible for wiping the returned plaintext buffer when finished.
func Decrypt(ciphertext []byte, nonce []byte, hexKey string) (plaintext []byte, err error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err = aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
