package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupChatHistoryTestEnv(t *testing.T) (*ChatHistoryController, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&models.Device{}, &models.ChatMessage{}); err != nil {
		t.Fatalf("failed to migrate models: %v", err)
	}

	device := &models.Device{
		UserID:     42,
		AgentID:    7,
		DeviceName: "dev-mcp-01",
		DeviceCode: "dev-mcp-01",
	}
	if err := db.Create(device).Error; err != nil {
		t.Fatalf("failed to create test device: %v", err)
	}

	ctrl := &ChatHistoryController{
		DB:            db,
		AudioBasePath: t.TempDir(),
	}

	r := gin.New()
	r.POST("/api/internal/history/messages", ctrl.SaveMessage)
	r.GET("/api/internal/history/messages", ctrl.GetMessagesForInit)

	authed := r.Group("/api/user", func(c *gin.Context) {
		c.Set("user_id", uint(42))
		c.Next()
	})
	authed.GET("/history/messages", ctrl.GetMessages)
	authed.GET("/history/agents/:agent_id/messages", ctrl.GetMessagesByAgent)
	authed.GET("/history/export", ctrl.ExportMessages)

	return ctrl, r
}

func postSaveMessage(t *testing.T, r *gin.Engine, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/history/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestChatHistoryMCPToolCallEndToEnd(t *testing.T) {
	_, r := setupChatHistoryTestEnv(t)

	// 1. User message
	if w := postSaveMessage(t, r, map[string]any{
		"message_id": "msg-user-1",
		"device_id":  "dev-mcp-01",
		"agent_id":   "7",
		"session_id": "sess-1",
		"role":       "user",
		"content":    "帮我查一下上海天气",
	}); w.Code != http.StatusCreated {
		t.Fatalf("save user message failed: status=%d body=%s", w.Code, w.Body.String())
	}

	// 2. Assistant message with empty content and tool_calls_json MUST succeed (issue #226)
	toolCallsJSON := `[{"id":"call_weather_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Shanghai\"}"}}]`
	if w := postSaveMessage(t, r, map[string]any{
		"message_id":      "msg-assistant-toolcall-1",
		"device_id":       "dev-mcp-01",
		"agent_id":        "7",
		"session_id":      "sess-1",
		"role":            "assistant",
		"content":         "",
		"tool_calls_json": toolCallsJSON,
	}); w.Code != http.StatusCreated {
		t.Fatalf("save assistant tool_calls message with empty content failed: status=%d body=%s", w.Code, w.Body.String())
	}

	// 3. Tool result message
	if w := postSaveMessage(t, r, map[string]any{
		"message_id":   "msg-tool-1",
		"device_id":    "dev-mcp-01",
		"agent_id":     "7",
		"session_id":   "sess-1",
		"role":         "tool",
		"content":      `{"city":"Shanghai","weather":"多云","temp":22}`,
		"tool_call_id": "call_weather_1",
	}); w.Code != http.StatusCreated {
		t.Fatalf("save tool message failed: status=%d body=%s", w.Code, w.Body.String())
	}

	// 4. Final assistant reply
	if w := postSaveMessage(t, r, map[string]any{
		"message_id": "msg-assistant-final-1",
		"device_id":  "dev-mcp-01",
		"agent_id":   "7",
		"session_id": "sess-1",
		"role":       "assistant",
		"content":    "上海今天多云，气温22度。",
	}); w.Code != http.StatusCreated {
		t.Fatalf("save final assistant message failed: status=%d body=%s", w.Code, w.Body.String())
	}

	// 5. Internal init endpoint MUST return all 4 messages with paired tool_calls -> tool
	reqInit := httptest.NewRequest(http.MethodGet, "/api/internal/history/messages?device_id=dev-mcp-01&agent_id=7&session_id=sess-1&limit=20", nil)
	wInit := httptest.NewRecorder()
	r.ServeHTTP(wInit, reqInit)
	if wInit.Code != http.StatusOK {
		t.Fatalf("GetMessagesForInit failed: status=%d body=%s", wInit.Code, wInit.Body.String())
	}
	var initResp struct {
		Messages []struct {
			Role       string           `json:"role"`
			Content    string           `json:"content"`
			ToolCallID string           `json:"tool_call_id"`
			ToolCalls  []map[string]any `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(wInit.Body.Bytes(), &initResp); err != nil {
		t.Fatalf("unmarshal GetMessagesForInit response: %v", err)
	}
	if len(initResp.Messages) != 4 {
		t.Fatalf("GetMessagesForInit len = %d, want 4; body=%s", len(initResp.Messages), wInit.Body.String())
	}
	if initResp.Messages[0].Role != "user" ||
		initResp.Messages[1].Role != "assistant" || len(initResp.Messages[1].ToolCalls) != 1 ||
		initResp.Messages[2].Role != "tool" || initResp.Messages[2].ToolCallID != "call_weather_1" ||
		initResp.Messages[3].Role != "assistant" || initResp.Messages[3].Content != "上海今天多云，气温22度。" {
		t.Fatalf("unexpected GetMessagesForInit sequence: %+v", initResp.Messages)
	}

	// 6. User-facing AgentHistory endpoint MUST hide tool messages and empty assistant tool_call rows
	for _, endpoint := range []string{
		"/api/user/history/agents/7/messages?page=1&page_size=50",
		"/api/user/history/messages?agent_id=7&page=1&page_size=50",
	} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s failed: status=%d body=%s", endpoint, w.Code, w.Body.String())
		}
		var resp struct {
			Total int64 `json:"total"`
			Data  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal %s response: %v", endpoint, err)
		}
		if resp.Total != 2 || len(resp.Data) != 2 {
			t.Fatalf("%s returned total=%d len=%d, want 2 (only user + visible assistant); body=%s",
				endpoint, resp.Total, len(resp.Data), w.Body.String())
		}
		for _, item := range resp.Data {
			if item.Role == "tool" || item.Content == "" {
				t.Errorf("%s leaked internal/empty message: %+v", endpoint, item)
			}
		}
	}

	// 7. Export endpoint MUST also exclude internal tool messages and empty assistant tool_calls
	reqExport := httptest.NewRequest(http.MethodGet, "/api/user/history/export?agent_id=7", nil)
	wExport := httptest.NewRecorder()
	r.ServeHTTP(wExport, reqExport)
	if wExport.Code != http.StatusOK {
		t.Fatalf("ExportMessages failed: status=%d body=%s", wExport.Code, wExport.Body.String())
	}
	var exportResp struct {
		Total    int `json:"total"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(wExport.Body.Bytes(), &exportResp); err != nil {
		t.Fatalf("unmarshal export response: %v", err)
	}
	if exportResp.Total != 2 || len(exportResp.Messages) != 2 {
		t.Fatalf("ExportMessages returned total=%d len=%d, want 2; body=%s", exportResp.Total, len(exportResp.Messages), wExport.Body.String())
	}
}

func TestSaveMessageValidation(t *testing.T) {
	_, r := setupChatHistoryTestEnv(t)

	// Empty user message must still be rejected
	if w := postSaveMessage(t, r, map[string]any{
		"message_id": "empty-user",
		"device_id":  "dev-mcp-01",
		"agent_id":   "7",
		"role":       "user",
		"content":    "",
	}); w.Code != http.StatusBadRequest {
		t.Errorf("empty user content status = %d, want 400", w.Code)
	}

	// Empty assistant message without tool_calls must be rejected
	if w := postSaveMessage(t, r, map[string]any{
		"message_id": "empty-assistant",
		"device_id":  "dev-mcp-01",
		"agent_id":   "7",
		"role":       "assistant",
		"content":    "   ",
	}); w.Code != http.StatusBadRequest {
		t.Errorf("empty assistant without tool_calls status = %d, want 400", w.Code)
	}

	// Empty assistant message with empty tool_calls array must be rejected
	if w := postSaveMessage(t, r, map[string]any{
		"message_id":      "empty-assistant-empty-array",
		"device_id":       "dev-mcp-01",
		"agent_id":        "7",
		"role":            "assistant",
		"content":         "",
		"tool_calls_json": "[]",
	}); w.Code != http.StatusBadRequest {
		t.Errorf("empty assistant with empty tool_calls array status = %d, want 400", w.Code)
	}
}
