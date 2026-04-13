package mqtt_server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	client "xiaozhi-esp32-server-golang/internal/data/msg"
	log "xiaozhi-esp32-server-golang/logger"
)

type mcpMessageEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type mcpResponsePayload struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	Method  string      `json:"method,omitempty"`
}

type pendingMcpRequest struct {
	requestID string
	ch        chan map[string]interface{}
}

var (
	mcpPublishPending        sync.Map // key: <topicMac>|<requestID>, value: chan map[string]interface{}
	mcpPublishPendingByTopic sync.Map // key: <topicMac>, value: *pendingMcpRequest
	mcpTopicLocks            sync.Map // key: <topicMac>, value: *sync.Mutex
	mcpRequestSeq            int64    = time.Now().UnixMilli()
)

func normalizeDeviceIDToTopicMAC(deviceID string) string {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ""
	}

	// 兼容 client_id 形态：GID@@@mac@@@uuid 或 @@@mac@@@mac
	if strings.Contains(deviceID, "@@@") {
		parts := strings.Split(deviceID, "@@@")
		if len(parts) >= 3 {
			candidate := strings.TrimSpace(parts[1])
			if candidate != "" {
				deviceID = candidate
			}
		}
	}

	deviceID = strings.ToLower(deviceID)
	deviceID = strings.ReplaceAll(deviceID, ":", "_")
	deviceID = strings.ReplaceAll(deviceID, "-", "_")
	return deviceID
}

func requestKey(topicMAC, requestID string) string {
	return topicMAC + "|" + requestID
}

func getTopicLock(topicMAC string) *sync.Mutex {
	if lock, ok := mcpTopicLocks.Load(topicMAC); ok {
		return lock.(*sync.Mutex)
	}
	newLock := &sync.Mutex{}
	actual, _ := mcpTopicLocks.LoadOrStore(topicMAC, newLock)
	return actual.(*sync.Mutex)
}

func parseRequestID(id interface{}) string {
	switch v := id.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func DispatchMcpPublishResponse(topic string, payload []byte) {
	if !strings.HasPrefix(topic, client.MDevicePubTopicPrefix) {
		return
	}

	var envelope mcpMessageEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Type != "mcp" || len(envelope.Payload) == 0 {
		return
	}

	var resp mcpResponsePayload
	if err := json.Unmarshal(envelope.Payload, &resp); err != nil {
		return
	}
	requestID := parseRequestID(resp.ID)
	if requestID == "" {
		return
	}

	topicMAC := strings.TrimPrefix(topic, client.MDevicePubTopicPrefix)
	key := requestKey(topicMAC, requestID)
	chAny, ok := mcpPublishPending.Load(key)
	if !ok {
		fallback, okByTopic := mcpPublishPendingByTopic.Load(topicMAC)
		if !okByTopic {
			return
		}
		pending := fallback.(*pendingMcpRequest)
		chAny = pending.ch
		log.Warnf("MQTT响应ID不匹配，按topic兜底匹配: topic=%s response_id=%s expect_id=%s", topic, requestID, pending.requestID)
	} else {
		log.Debugf("MQTT收到MCP响应: topic=%s request_id=%s", topic, requestID)
	}
	ch := chAny.(chan map[string]interface{})

	responseBody := map[string]interface{}{}
	if err := json.Unmarshal(payload, &responseBody); err != nil {
		responseBody = map[string]interface{}{"raw": string(payload)}
	}

	select {
	case ch <- responseBody:
	default:
	}
}

func PublishMcpMessageAndWaitResponse(ctx context.Context, deviceID, method string, params map[string]interface{}, timeoutMs int) (map[string]interface{}, error) {
	deviceID = strings.TrimSpace(deviceID)
	method = strings.TrimSpace(method)
	if deviceID == "" {
		return nil, fmt.Errorf("device_id不能为空")
	}
	if method == "" {
		return nil, fmt.Errorf("method不能为空")
	}
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}
	if ctx == nil {
		ctx = context.Background()
	}

	topicMAC := normalizeDeviceIDToTopicMAC(deviceID)
	if topicMAC == "" {
		return nil, fmt.Errorf("device_id格式无效")
	}
	topic := client.MDeviceSubTopicPrefix + topicMAC
	responseTopic := client.MDevicePubTopicPrefix + topicMAC

	serverMu.Lock()
	srv := currentServer
	serverMu.Unlock()
	if srv == nil {
		return nil, fmt.Errorf("MQTT服务未启用")
	}

	lock := getTopicLock(topicMAC)
	lock.Lock()
	defer lock.Unlock()

	requestIDNum := atomic.AddInt64(&mcpRequestSeq, 1)
	requestID := strconv.FormatInt(requestIDNum, 10)
	waitKey := requestKey(topicMAC, requestID)
	respCh := make(chan map[string]interface{}, 1)
	mcpPublishPending.Store(waitKey, respCh)
	mcpPublishPendingByTopic.Store(topicMAC, &pendingMcpRequest{requestID: requestID, ch: respCh})
	defer mcpPublishPending.Delete(waitKey)
	defer mcpPublishPendingByTopic.Delete(topicMAC)

	message := map[string]interface{}{
		"type": "mcp",
		"payload": map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      requestIDNum,
			"method":  method,
			"params":  params,
		},
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("序列化MCP消息失败: %w", err)
	}

	log.Debugf("MQTT下发MCP请求: topic=%s request_id=%s method=%s", topic, requestID, method)
	if err = srv.Publish(topic, payload, false, 0); err != nil {
		return nil, fmt.Errorf("MQTT发布失败: %w", err)
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return map[string]interface{}{
			"device_id":      deviceID,
			"topic":          topic,
			"response_topic": responseTopic,
			"request_id":     requestID,
			"response":       resp,
			"timeout_ms":     timeoutMs,
		}, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("等待设备响应超时")
	}
}
