package doubao

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDoubaoWSTTSOnline(t *testing.T) {
	if os.Getenv("RUN_ONLINE_TESTS") != "1" {
		t.Skip("skip online TTS tests: set RUN_ONLINE_TESTS=1")
	}

	provider := NewDoubaoWSProvider(map[string]interface{}{
		"appid":        requireEnv(t, "TTS_DOUBAO_APPID"),
		"access_token": requireEnv(t, "TTS_DOUBAO_ACCESS_TOKEN"),
		"cluster":      getenvOrDefault("TTS_DOUBAO_CLUSTER", "volcano_tts"),
		"voice":        getenvOrDefault("TTS_DOUBAO_VOICE", "BV001_streaming"),
		"ws_host":      getenvOrDefault("TTS_DOUBAO_WS_HOST", "openspeech.bytedance.com"),
		"use_stream":   true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := provider.TextToSpeechStream(ctx, "在线测试豆包WebSocket TTS", 16000, 1, 60)
	if err != nil {
		t.Fatalf("doubao ws TextToSpeechStream failed: %v", err)
	}

	select {
	case frame, ok := <-output:
		if !ok {
			t.Fatalf("doubao ws output channel closed without data")
		}
		if len(frame) == 0 {
			t.Fatalf("doubao ws returned empty frame")
		}
	case <-ctx.Done():
		t.Fatalf("doubao ws stream timeout: %v", ctx.Err())
	}
}
