package notify

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrTokenExpired = errors.New("notify token has expired")
	ErrTokenInvalid = errors.New("notify token is invalid")
)

// deriveKey 确保密钥为 32 字节 (AES-256)
func deriveKey(key []byte) []byte {
	if len(key) == 32 {
		return key
	}
	hash := sha256.Sum256(key)
	return hash[:]
}

// EncryptToken 使用 AES-256-GCM 将 NotifyTokenPayload 加密为 Base64URL 字符串
func EncryptToken(payload *NotifyTokenPayload, secretKey []byte) (string, error) {
	if payload == nil {
		return "", errors.New("payload cannot be nil")
	}
	if len(secretKey) == 0 {
		return "", errors.New("secretKey cannot be empty")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal token payload failed: %w", err)
	}

	key := deriveKey(secretKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher block failed: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM failed: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce failed: %w", err)
	}

	// nonce + ciphertext + tag
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// DecryptToken 解密 Base64URL 字符串并校验过期时间
func DecryptToken(tokenStr string, secretKey []byte) (*NotifyTokenPayload, error) {
	if tokenStr == "" {
		return nil, errors.New("token cannot be empty")
	}
	if len(secretKey) == 0 {
		return nil, errors.New("secretKey cannot be empty")
	}

	ciphertext, err := base64.RawURLEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode failed", ErrTokenInvalid)
	}

	key := deriveKey(secretKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher block failed: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM failed: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("%w: ciphertext too short", ErrTokenInvalid)
	}

	nonce, encryptedData := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: gcm decrypt failed", ErrTokenInvalid)
	}

	var payload NotifyTokenPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("%w: unmarshal payload failed", ErrTokenInvalid)
	}

	if payload.ExpiresAt > 0 && time.Now().Unix() > payload.ExpiresAt {
		return nil, ErrTokenExpired
	}

	return &payload, nil
}
