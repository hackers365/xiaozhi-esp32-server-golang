package notify

import (
	"testing"
	"time"
)

func TestTokenEncryptDecrypt(t *testing.T) {
	secretKey := []byte("my_secret_key_123456")
	now := time.Now().Unix()
	payload := &NotifyTokenPayload{
		UUID:      "test-uuid-1234-5678",
		ExpiresAt: now + 600,
		Nonce:     "nonce-abc-123",
	}

	tokenStr, err := EncryptToken(payload, secretKey)
	if err != nil {
		t.Fatalf("EncryptToken failed: %v", err)
	}

	if tokenStr == "" {
		t.Fatal("expected non-empty token string")
	}

	decrypted, err := DecryptToken(tokenStr, secretKey)
	if err != nil {
		t.Fatalf("DecryptToken failed: %v", err)
	}

	if decrypted.UUID != payload.UUID {
		t.Fatalf("expected UUID %s, got %s", payload.UUID, decrypted.UUID)
	}
	if decrypted.ExpiresAt != payload.ExpiresAt {
		t.Fatalf("expected ExpiresAt %d, got %d", payload.ExpiresAt, decrypted.ExpiresAt)
	}
	if decrypted.Nonce != payload.Nonce {
		t.Fatalf("expected Nonce %s, got %s", payload.Nonce, decrypted.Nonce)
	}
}

func TestTokenExpired(t *testing.T) {
	secretKey := []byte("my_secret_key_123456")
	payload := &NotifyTokenPayload{
		UUID:      "test-uuid-expired",
		ExpiresAt: time.Now().Unix() - 10, // expired
		Nonce:     "nonce-1",
	}

	tokenStr, err := EncryptToken(payload, secretKey)
	if err != nil {
		t.Fatalf("EncryptToken failed: %v", err)
	}

	_, err = DecryptToken(tokenStr, secretKey)
	if err == nil || err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestTokenInvalidKeyOrCiphertext(t *testing.T) {
	secretKey1 := []byte("key_1")
	secretKey2 := []byte("key_2")

	payload := &NotifyTokenPayload{
		UUID:      "test-uuid",
		ExpiresAt: time.Now().Unix() + 100,
		Nonce:     "nonce",
	}

	tokenStr, err := EncryptToken(payload, secretKey1)
	if err != nil {
		t.Fatalf("EncryptToken failed: %v", err)
	}

	_, err = DecryptToken(tokenStr, secretKey2)
	if err == nil {
		t.Fatal("expected decryption with wrong key to fail")
	}

	_, err = DecryptToken("invalid-base64-!@#", secretKey1)
	if err == nil {
		t.Fatal("expected invalid token to fail")
	}
}
