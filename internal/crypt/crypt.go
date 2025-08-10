package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"

	"telegram-chatgpt-bot/internal/logging"
)

var aesGCM cipher.AEAD

// Init sets up the AES-GCM cipher using the TBOT_MASTER_KEY env var.
func Init() {
	keyB64 := os.Getenv("TBOT_MASTER_KEY")
	if keyB64 == "" {
		logging.Log.Fatal().Msg("TBOT_MASTER_KEY env var is required (base64-encoded 32 bytes)")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil || len(key) != 32 {
		logging.Log.Fatal().Err(err).Msg("invalid TBOT_MASTER_KEY")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		logging.Log.Fatal().Err(err).Msg("aes.NewCipher")
	}
	aesGCM, err = cipher.NewGCM(block)
	if err != nil {
		logging.Log.Fatal().Err(err).Msg("cipher.NewGCM")
	}
}

// Encrypt returns a base64 ciphertext of the provided plaintext.
func Encrypt(plain string) (string, error) {
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := aesGCM.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt converts a base64 ciphertext back to plaintext.
func Decrypt(ciphertextB64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", err
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	pt, err := aesGCM.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
