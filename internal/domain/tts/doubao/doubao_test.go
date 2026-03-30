package doubao

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDoubaoTTSOnline(t *testing.T) {
	if os.Getenv("RUN_ONLINE_TESTS") != "1" {
		t.Skip("skip online TTS tests: set RUN_ONLINE_TESTS=1")
	}

	provider := NewDoubaoTTSProvider(map[string]interface{}{
		"api_url":       getenvOrDefault("TTS_DOUBAO_API_URL", "https://openspeech.bytedance.com/api/v1/tts"),
		"voice":         getenvOrDefault("TTS_DOUBAO_VOICE", "BV001_streaming"),
		"authorization": getenvOrDefault("TTS_DOUBAO_AUTHORIZATION", "Bearer;"),
		"appid":         requireEnv(t, "TTS_DOUBAO_APPID"),
		"access_token":  requireEnv(t, "TTS_DOUBAO_ACCESS_TOKEN"),
		"cluster":       getenvOrDefault("TTS_DOUBAO_CLUSTER", "volcano_tts"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	frames, err := provider.TextToSpeech(ctx, "在线测试豆包TTS", 16000, 1, 60)
	if err != nil {
		t.Fatalf("doubao TextToSpeech failed: %v", err)
	}
	if len(frames) == 0 {
		t.Fatalf("doubao TextToSpeech returned empty frames")
	}
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skip Doubao TTS online test: missing %s", key)
	}
	return v
}

func getenvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
