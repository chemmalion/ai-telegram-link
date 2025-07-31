package main

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "io"
    "log"
    "os"
)

var aesGCM cipher.AEAD

func initCipher() {
    keyB64 := os.Getenv("MASTER_KEY")
    if keyB64 == "" {
        log.Fatal("MASTER_KEY env var is required (base64-encoded 32 bytes)")
    }
    key, err := base64.StdEncoding.DecodeString(keyB64)
    if err != nil || len(key) != 32 {
        log.Fatalf("invalid MASTER_KEY: %v", err)
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        log.Fatal(err)
    }
    aesGCM, err = cipher.NewGCM(block)
    if err != nil {
        log.Fatal(err)
    }
}

func encrypt(plain string) (string, error) {
    nonce := make([]byte, aesGCM.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }
    ct := aesGCM.Seal(nonce, nonce, []byte(plain), nil)
    return base64.StdEncoding.EncodeToString(ct), nil
}

func decrypt(ciphertextB64 string) (string, error) {
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
