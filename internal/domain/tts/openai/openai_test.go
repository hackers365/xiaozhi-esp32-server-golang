package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/util"
)

func TestOpenAITTSOnline(t *testing.T) {
	if os.Getenv("RUN_ONLINE_TESTS") != "1" {
		t.Skip("skip online TTS tests: set RUN_ONLINE_TESTS=1")
	}

	apiKey := os.Getenv("TTS_OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		t.Skip("skip OpenAI TTS online test: missing TTS_OPENAI_API_KEY or OPENAI_API_KEY")
	}

	provider := NewOpenAITTSProvider(map[string]interface{}{
		"api_key":         apiKey,
		"model":           "tts-1",
		"voice":           getenvOrDefault("TTS_OPENAI_VOICE", "alloy"),
		"response_format": "mp3",
		"speed":           1.0,
	})

	t.Run("text_to_speech", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		frames, err := provider.TextToSpeech(ctx, "online openai tts test", 16000, 1, 60)
		if err != nil {
			t.Fatalf("TextToSpeech failed: %v", err)
		}
		if len(frames) == 0 {
			t.Fatalf("TextToSpeech returned empty frames")
		}
	})

	t.Run("text_to_speech_stream", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		stream, err := provider.TextToSpeechStream(ctx, "online openai stream tts test", 16000, 1, 60)
		if err != nil {
			t.Fatalf("TextToSpeechStream failed: %v", err)
		}

		select {
		case frame, ok := <-stream:
			if !ok {
				t.Fatalf("stream closed without data")
			}
			if len(frame) == 0 {
				t.Fatalf("received empty frame")
			}
		case <-ctx.Done():
			t.Fatalf("stream timeout: %v", ctx.Err())
		}
	})
}

func getenvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestOpenAITTSProviderSupportsOpusResponse(t *testing.T) {
	sampleRate := 16000
	pcm := make([]int16, sampleRate/2)
	for i := range pcm {
		if i%32 < 16 {
			pcm[i] = 2400
		} else {
			pcm[i] = -2400
		}
	}

	opusBytes, err := util.PCM16ToOggOpus(pcm, sampleRate, 1, 20)
	if err != nil {
		t.Fatalf("生成测试 Ogg Opus 失败: %v", err)
	}

	requestErrCh := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			requestErrCh <- err
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.ResponseFormat != "opus" {
			requestErrCh <- fmt.Errorf("期望 response_format=opus，实际为 %s", req.ResponseFormat)
			http.Error(w, "unexpected response_format", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "audio/ogg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(opusBytes)
	}))
	defer server.Close()

	provider := NewOpenAITTSProvider(map[string]interface{}{
		"api_url":         server.URL,
		"model":           "tts-1",
		"voice":           "alloy",
		"response_format": "opus",
		"speed":           1.0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	outputChan, err := provider.TextToSpeechStream(ctx, "测试 opus 输出", sampleRate, 1, 60)
	if err != nil {
		t.Fatalf("TextToSpeechStream 返回错误: %v", err)
	}

	frameCount := 0
	for frame := range outputChan {
		if len(frame) == 0 {
			t.Fatal("收到空 Opus 帧")
		}
		frameCount++
	}

	if frameCount == 0 {
		t.Fatal("未收到任何 Opus 帧")
	}

	select {
	case err := <-requestErrCh:
		t.Fatalf("mock server 校验失败: %v", err)
	default:
	}
}
