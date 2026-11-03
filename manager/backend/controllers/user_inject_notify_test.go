package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
)

type stubInjectWSController struct {
	lastParams map[string]interface{}
}

func (s *stubInjectWSController) RequestMcpToolDetailsFromClient(ctx context.Context, agentID string) ([]MCPTool, error) {
	return nil, nil
}
func (s *stubInjectWSController) RequestMcpEndpointStatusFromClient(ctx context.Context, agentID string) (map[string]interface{}, error) {
	return nil, nil
}
func (s *stubInjectWSController) RequestDeviceMcpToolDetailsFromClient(ctx context.Context, deviceID string) ([]MCPTool, error) {
	return nil, nil
}
func (s *stubInjectWSController) CallMcpToolFromClient(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error) {
	return nil, nil
}
func (s *stubInjectWSController) RequestOpenClawStatusFromClient(ctx context.Context, agentID string) (map[string]interface{}, error) {
	return nil, nil
}
func (s *stubInjectWSController) CallOpenClawChatFromClient(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error) {
	return nil, nil
}
func (s *stubInjectWSController) CallOpenClawChatStreamFromClient(ctx context.Context, body map[string]interface{}, onResponse func(*WebSocketResponse) error) (map[string]interface{}, error) {
	return nil, nil
}
func (s *stubInjectWSController) InjectMessageToDevice(ctx context.Context, deviceID, message string, skipLlm bool, autoListen bool) error {
	return nil
}
func (s *stubInjectWSController) InjectMessageParamsToDevice(ctx context.Context, params map[string]interface{}) error {
	s.lastParams = params
	return nil
}

func TestUserInjectMessageAllowsAudioURLOnlyInNotifyMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAgentDeviceServiceTestDB(t)
	if err := db.Create(&models.Device{
		UserID:     7,
		DeviceName: "aa:bb:cc:dd:ee:01",
	}).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}

	wsStub := &stubInjectWSController{}
	uc := &UserController{
		DB:                  db,
		WebSocketController: wsStub,
	}

	router := gin.New()
	router.POST("/api/user/devices/inject-message", func(c *gin.Context) {
		c.Set("user_id", uint(7))
		uc.InjectMessage(c)
	})

	// 1. notify 模式仅传合法 audio_url（不传 message）应成功
	body1, _ := json.Marshal(map[string]interface{}{
		"device_id": "aa:bb:cc:dd:ee:01",
		"mode":      "notify",
		"audio_url": "https://cdn.example.com/dingdong.ogg",
	})
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodPost, "/api/user/devices/inject-message", bytes.NewReader(body1)))
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for audio_url-only notify, got %d body=%s", rec1.Code, rec1.Body.String())
	}
	if wsStub.lastParams["audio_url"] != "https://cdn.example.com/dingdong.ogg" || wsStub.lastParams["mode"] != "notify" {
		t.Fatalf("unexpected forwarded params: %+v", wsStub.lastParams)
	}

	// 2. notify 模式传入非法 scheme 的 audio_url 应返回 400
	body2, _ := json.Marshal(map[string]interface{}{
		"device_id": "aa:bb:cc:dd:ee:01",
		"mode":      "notify",
		"audio_url": "file:///etc/passwd",
	})
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/api/user/devices/inject-message", bytes.NewReader(body2)))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for non-http audio_url, got %d", rec2.Code)
	}

	// 3. 默认 speak 模式不传 message 仍应返回 400
	body3, _ := json.Marshal(map[string]interface{}{
		"device_id": "aa:bb:cc:dd:ee:01",
		"audio_url": "https://cdn.example.com/dingdong.ogg",
	})
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, httptest.NewRequest(http.MethodPost, "/api/user/devices/inject-message", bytes.NewReader(body3)))
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request when message is empty in speak mode, got %d", rec3.Code)
	}
}
