package doubaoapi

import "testing"

func TestNewTTSHeadersLegacy(t *testing.T) {
	headers := NewTTSHeaders("app-123", "token-456", "seed-tts-2.0")

	if got := headers.Get("X-Api-Key"); got != "" {
		t.Fatalf("expected empty X-Api-Key in legacy mode, got %q", got)
	}
	if got := headers.Get("X-Api-App-Id"); got != "app-123" {
		t.Fatalf("X-Api-App-Id = %q, want %q", got, "app-123")
	}
	if got := headers.Get("X-Api-Access-Key"); got != "token-456" {
		t.Fatalf("X-Api-Access-Key = %q, want %q", got, "token-456")
	}
	if got := headers.Get("Authorization"); got != "Bearer;token-456" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer;token-456")
	}
	if got := headers.Get("X-Api-Resource-Id"); got != "seed-tts-2.0" {
		t.Fatalf("X-Api-Resource-Id = %q, want %q", got, "seed-tts-2.0")
	}
	if got := headers.Get("X-Api-Request-Id"); got == "" {
		t.Fatal("expected non-empty X-Api-Request-Id")
	}
}

func TestNewTTSHeadersWithAPIKey(t *testing.T) {
	headers := NewTTSHeadersWithAuth("  my-api-key  ", "ignored-app", "ignored-token", "seed-tts-2.0")

	if got := headers.Get("X-Api-Key"); got != "my-api-key" {
		t.Fatalf("X-Api-Key = %q, want %q", got, "my-api-key")
	}
	if got := headers.Get("X-Api-App-Id"); got != "" {
		t.Fatalf("expected empty X-Api-App-Id when api_key is set, got %q", got)
	}
	if got := headers.Get("X-Api-Access-Key"); got != "" {
		t.Fatalf("expected empty X-Api-Access-Key when api_key is set, got %q", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Fatalf("expected empty Authorization when api_key is set, got %q", got)
	}
	if got := headers.Get("X-Api-Resource-Id"); got != "seed-tts-2.0" {
		t.Fatalf("X-Api-Resource-Id = %q, want %q", got, "seed-tts-2.0")
	}
	if got := headers.Get("X-Api-Request-Id"); got == "" {
		t.Fatal("expected non-empty X-Api-Request-Id")
	}
}

func TestNewTTSWebsocketHeadersWithAPIKey(t *testing.T) {
	headers := NewTTSWebsocketHeadersWithAuth("ws-api-key", "", "", "seed-tts-2.0", "conn-1")

	if got := headers.Get("X-Api-Key"); got != "ws-api-key" {
		t.Fatalf("X-Api-Key = %q, want %q", got, "ws-api-key")
	}
	if got := headers.Get("X-Api-Connect-Id"); got != "conn-1" {
		t.Fatalf("X-Api-Connect-Id = %q, want %q", got, "conn-1")
	}
	if got := headers.Get("X-Api-App-Id"); got != "" {
		t.Fatalf("expected empty X-Api-App-Id, got %q", got)
	}
}
