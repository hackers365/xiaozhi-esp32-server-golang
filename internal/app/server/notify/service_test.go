package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockTTSProvider struct{}

func (m *mockTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	ch := make(chan []byte, 5)
	go func() {
		ch <- []byte{0xf8, 0x01, 0x02}
		ch <- []byte{0xf8, 0x03, 0x04}
		close(ch)
	}()
	return ch, nil
}

func (m *mockTTSProvider) Close() error {
	return nil
}

func (m *mockTTSProvider) IsValid() bool {
	return true
}

func TestNotifyServiceFullFlow(t *testing.T) {
	secretKey := []byte("test_secret_32_bytes_long_123456")
	publicBaseURL := "http://192.168.1.100:8990"

	mockResolver := func(ctx context.Context, config *NotifyConfig) (TTSStreamProvider, func(), error) {
		p := &mockTTSProvider{}
		return p, func() {}, nil
	}

	service := NewNotifyService(secretKey, publicBaseURL, nil, mockResolver)

	// 1. 准备通知
	req := PrepareNotifyRequest{
		DeviceID: "test-dev-001",
		Text:     "主人，任务已经完成了。请查收！",
		Voice:    "zh_female_cancan",
		Speed:    1.0,
	}

	result, err := service.PrepareNotify(context.Background(), req)
	if err != nil {
		t.Fatalf("PrepareNotify failed: %v", err)
	}

	if result.AudioURL == "" {
		t.Fatal("expected non-empty AudioURL")
	}
	if len(result.Subtitles) != 2 {
		t.Fatalf("expected 2 subtitles, got %d", len(result.Subtitles))
	}
	if result.UUID == "" {
		t.Fatal("expected non-empty UUID")
	}

	// 2. 模拟 ESP32 发起 HTTP GET 请求
	httpReq := httptest.NewRequest("GET", result.AudioURL, nil)
	recorder := httptest.NewRecorder()

	service.HandleStream(recorder, httpReq)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "audio/ogg" {
		t.Fatalf("expected Content-Type audio/ogg, got %s", contentType)
	}

	bodyBytes := recorder.Body.Bytes()
	if len(bodyBytes) == 0 {
		t.Fatal("expected non-empty response body")
	}

	if string(bodyBytes[:4]) != "OggS" {
		t.Fatalf("expected body starting with OggS, got %s", string(bodyBytes[:4]))
	}
}

func TestNotifyServiceInvalidToken(t *testing.T) {
	service := NewNotifyService([]byte("key"), "http://localhost", nil, nil)
	httpReq := httptest.NewRequest("GET", "/xiaozhi/notify/stream?token=invalid", nil)
	recorder := httptest.NewRecorder()

	service.HandleStream(recorder, httpReq)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", recorder.Code)
	}
}
