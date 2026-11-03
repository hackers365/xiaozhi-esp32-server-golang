package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type mockTTSProvider struct {
	streamFn func(ctx context.Context) (chan []byte, error)
}

func (m *mockTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx)
	}
	ch := make(chan []byte, 5)
	go func() {
		ch <- []byte{0x58, 0x01, 0x02}
		ch <- []byte{0x58, 0x03, 0x04}
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
	defer service.Close()

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
	httpReq := httptest.NewRequest(http.MethodGet, result.AudioURL, nil)
	recorder := httptest.NewRecorder()

	service.HandleStream(recorder, httpReq)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", resp.StatusCode)
	}
	if te := resp.Header.Get("Transfer-Encoding"); te != "" {
		t.Fatalf("should not manually set Transfer-Encoding header, got %q", te)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "audio/ogg" {
		t.Fatalf("expected Content-Type audio/ogg, got %s", contentType)
	}

	pages := parseAndVerifyOggStream(t, recorder.Body.Bytes())
	if len(pages) != 4 {
		t.Fatalf("expected 4 Ogg pages (head, tags, 2 frames), got %d", len(pages))
	}
}

func TestNotifyServiceInvalidTokenAndMethod(t *testing.T) {
	service := NewNotifyService(nil, "http://localhost", nil, nil)
	defer service.Close()

	// 非 GET 请求返回 405
	postReq := httptest.NewRequest(http.MethodPost, "/xiaozhi/notify/stream?token=abc", nil)
	postRec := httptest.NewRecorder()
	service.HandleStream(postRec, postReq)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405 for POST, got %d", postRec.Code)
	}

	// 非法 Token 返回 403
	httpReq := httptest.NewRequest(http.MethodGet, "/xiaozhi/notify/stream?token=invalid", nil)
	recorder := httptest.NewRecorder()

	service.HandleStream(recorder, httpReq)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", recorder.Code)
	}
}

func TestNotifyServiceUpstreamErrorReturns502InsteadOf200(t *testing.T) {
	secretKey := []byte("test_secret_32_bytes_long_123456")

	t.Run("TextToSpeechStream error", func(t *testing.T) {
		service := NewNotifyService(secretKey, "http://192.168.1.100:8990", nil, func(ctx context.Context, config *NotifyConfig) (TTSStreamProvider, func(), error) {
			return &mockTTSProvider{
				streamFn: func(ctx context.Context) (chan []byte, error) {
					return nil, errors.New("upstream quota exceeded")
				},
			}, func() {}, nil
		})
		defer service.Close()

		res, err := service.PrepareNotify(context.Background(), PrepareNotifyRequest{
			DeviceID: "dev-1",
			Text:     "测试上游报错",
		})
		if err != nil {
			t.Fatalf("PrepareNotify failed: %v", err)
		}

		rec := httptest.NewRecorder()
		service.HandleStream(rec, httptest.NewRequest(http.MethodGet, res.AudioURL, nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("expected 502 Bad Gateway on stream start failure, got %d", rec.Code)
		}
	})

	t.Run("Empty audio stream", func(t *testing.T) {
		service := NewNotifyService(secretKey, "http://192.168.1.100:8990", nil, func(ctx context.Context, config *NotifyConfig) (TTSStreamProvider, func(), error) {
			return &mockTTSProvider{
				streamFn: func(ctx context.Context) (chan []byte, error) {
					ch := make(chan []byte)
					close(ch)
					return ch, nil
				},
			}, func() {}, nil
		})
		defer service.Close()

		res, err := service.PrepareNotify(context.Background(), PrepareNotifyRequest{
			DeviceID: "dev-1",
			Text:     "测试空音频流",
		})
		if err != nil {
			t.Fatalf("PrepareNotify failed: %v", err)
		}

		rec := httptest.NewRecorder()
		service.HandleStream(rec, httptest.NewRequest(http.MethodGet, res.AudioURL, nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("expected 502 Bad Gateway on empty stream, got %d", rec.Code)
		}
	})
}

type failingWriteResponseWriter struct {
	header http.Header
	code   int
}

func (f *failingWriteResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}

func (f *failingWriteResponseWriter) WriteHeader(statusCode int) {
	f.code = statusCode
}

func (f *failingWriteResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client closed connection")
}

func TestNotifyServiceCancelsStreamCtxBeforePoolRelease(t *testing.T) {
	secretKey := []byte("test_secret_32_bytes_long_123456")
	var streamCtxSeen context.Context
	var ctxDoneAtRelease atomic.Bool

	service := NewNotifyService(secretKey, "http://192.168.1.100:8990", nil, func(ctx context.Context, config *NotifyConfig) (TTSStreamProvider, func(), error) {
		streamCtxSeen = ctx
		p := &mockTTSProvider{
			streamFn: func(ctx context.Context) (chan []byte, error) {
				ch := make(chan []byte, 2)
				ch <- []byte{0x58, 0x01, 0x02}
				return ch, nil
			},
		}
		release := func() {
			if streamCtxSeen != nil && streamCtxSeen.Err() != nil {
				ctxDoneAtRelease.Store(true)
			}
		}
		return p, release, nil
	})
	defer service.Close()

	res, err := service.PrepareNotify(context.Background(), PrepareNotifyRequest{
		DeviceID: "dev-cancel",
		Text:     "测试客户端提前断连",
	})
	if err != nil {
		t.Fatalf("PrepareNotify failed: %v", err)
	}

	// 模拟 ESP32 在收到响应体写出时立即断开 HTTP 连接（此时 r.Context() 尚未被调用方取消）
	fw := &failingWriteResponseWriter{}
	service.HandleStream(fw, httptest.NewRequest(http.MethodGet, res.AudioURL, nil))

	if !ctxDoneAtRelease.Load() {
		t.Fatal("expected streamCtx to be cancelled BEFORE releaseFunc returns provider to pool")
	}
}

func TestNotifyServiceExternalAudioURLValidation(t *testing.T) {
	service := NewNotifyService(nil, "http://192.168.1.100:8990", nil, nil)
	defer service.Close()

	if _, err := service.PrepareNotify(context.Background(), PrepareNotifyRequest{
		DeviceID: "dev-1",
		AudioURL: "ftp://example.com/test.ogg",
	}); err == nil {
		t.Fatal("expected invalid scheme audio_url to fail")
	}

	res, err := service.PrepareNotify(context.Background(), PrepareNotifyRequest{
		DeviceID: "dev-1",
		AudioURL: "https://cdn.example.com/notify.ogg",
		Text:     "门铃响了",
	})
	if err != nil {
		t.Fatalf("expected valid https audio_url to succeed, got %v", err)
	}
	if res.AudioURL != "https://cdn.example.com/notify.ogg" || len(res.Subtitles) == 0 {
		t.Fatalf("unexpected PrepareNotify result: %+v", res)
	}
}
