package asr

import (
	"context"
	"os"
	"testing"
	"time"
)

func onlineEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_ONLINE_TESTS") != "1" {
		t.Skip("skip online ASR tests: set RUN_ONLINE_TESTS=1")
	}
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skip online ASR test: missing %s", key)
	}
	return v
}

func runASRStreamingSmoke(t *testing.T, provider string, cfg map[string]interface{}) {
	t.Helper()
	asrProvider, err := NewAsrProvider(provider, cfg)
	if err != nil {
		t.Fatalf("create provider failed: %v", err)
	}
	defer asrProvider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	audio := make(chan []float32, 1)
	audio <- make([]float32, 1600)
	close(audio)

	resultCh, err := asrProvider.StreamingRecognize(ctx, audio)
	if err != nil {
		t.Fatalf("streaming recognize failed: %v", err)
	}

	select {
	case _, ok := <-resultCh:
		if !ok {
			t.Log("result channel closed without payload")
		}
	case <-ctx.Done():
		t.Fatalf("streaming timed out: %v", ctx.Err())
	}
}

func TestASROnlineProviders(t *testing.T) {
	onlineEnabled(t)

	t.Run("funasr", func(t *testing.T) {
		runASRStreamingSmoke(t, "funasr", map[string]interface{}{
			"host": os.Getenv("ASR_FUNASR_HOST"),
			"port": os.Getenv("ASR_FUNASR_PORT"),
		})
	})

	t.Run("aliyun_funasr", func(t *testing.T) {
		runASRStreamingSmoke(t, "aliyun_funasr", map[string]interface{}{
			"api_key": requireEnv(t, "ASR_ALIYUN_FUNASR_API_KEY"),
			"ws_url":  os.Getenv("ASR_ALIYUN_FUNASR_WS_URL"),
			"model":   os.Getenv("ASR_ALIYUN_FUNASR_MODEL"),
		})
	})

	t.Run("doubao", func(t *testing.T) {
		runASRStreamingSmoke(t, "doubao", map[string]interface{}{
			"appid":        requireEnv(t, "ASR_DOUBAO_APPID"),
			"access_token": requireEnv(t, "ASR_DOUBAO_ACCESS_TOKEN"),
			"ws_url":       os.Getenv("ASR_DOUBAO_WS_URL"),
			"resource_id":  os.Getenv("ASR_DOUBAO_RESOURCE_ID"),
			"model_name":   os.Getenv("ASR_DOUBAO_MODEL_NAME"),
		})
	})

	t.Run("aliyun_qwen3", func(t *testing.T) {
		runASRStreamingSmoke(t, "aliyun_qwen3", map[string]interface{}{
			"api_key": requireEnv(t, "ASR_ALIYUN_QWEN3_API_KEY"),
			"ws_url":  os.Getenv("ASR_ALIYUN_QWEN3_WS_URL"),
			"model":   os.Getenv("ASR_ALIYUN_QWEN3_MODEL"),
		})
	})

	t.Run("xunfei", func(t *testing.T) {
		runASRStreamingSmoke(t, "xunfei", map[string]interface{}{
			"appid":      requireEnv(t, "ASR_XUNFEI_APPID"),
			"api_key":    requireEnv(t, "ASR_XUNFEI_API_KEY"),
			"api_secret": requireEnv(t, "ASR_XUNFEI_API_SECRET"),
		})
	})
}
