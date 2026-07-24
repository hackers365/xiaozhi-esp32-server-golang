package local_asr_server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func mockAsrServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// 1. 发送连接握手响应
		connResp := ServerResponse{
			Type:      "connection",
			Message:   "WebSocket connected, ready for audio",
			SessionID: "test-session-123",
		}
		respBytes, _ := json.Marshal(connResp)
		_ = conn.WriteMessage(websocket.TextMessage, respBytes)

		// 2. 读取音频数据
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			if mt == websocket.BinaryMessage && len(message) > 0 {
				// 模拟收到音频并识别成功，返回 final 响应
				finalResp := ServerResponse{
					Type:      "final",
					Text:      "测试语音识别结果",
					Timestamp: time.Now().UnixMilli(),
					SessionID: "test-session-123",
				}
				finalBytes, _ := json.Marshal(finalResp)
				_ = conn.WriteMessage(websocket.TextMessage, finalBytes)
				break
			}
		}
	}))
}

func TestLocalAsrServer_StreamingRecognize(t *testing.T) {
	ts := mockAsrServer(t)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	server, err := NewLocalAsrServer(Config{
		WsURL:   wsURL,
		Timeout: 5,
	})
	if err != nil {
		t.Fatalf("Failed to create LocalAsrServer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	audioChan := make(chan []float32, 2)
	// 发送模拟 PCM 音频切片 (160 采样点)
	audioChan <- make([]float32, 160)
	close(audioChan)

	resultChan, err := server.StreamingRecognize(ctx, audioChan)
	if err != nil {
		t.Fatalf("StreamingRecognize failed: %v", err)
	}

	var results []string
	for res := range resultChan {
		if res.Error != nil {
			t.Errorf("Unexpected error in result: %v", res.Error)
		}
		if res.Text != "" {
			results = append(results, res.Text)
		}
	}

	if len(results) == 0 {
		t.Fatalf("Expected recognition results, got empty")
	}

	if results[0] != "测试语音识别结果" {
		t.Errorf("Expected '测试语音识别结果', got '%s'", results[0])
	}
}
