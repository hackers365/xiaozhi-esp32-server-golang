package chat

import (
	"context"
	"encoding/json"
	"testing"

	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	. "xiaozhi-esp32-server-golang/internal/data/msg"
)

type brokerAwareNotifyTestConn struct {
	*sessionCloseTestConn
	online bool
}

func (c *brokerAwareNotifyTestConn) IsBrokerOnline() bool {
	return c.online
}

func TestChatManagerSendNotifyChecksBrokerOnlineAndClosedTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	baseConn := &sessionCloseTestConn{
		deviceID:      "dev-mqtt-1",
		transportType: types_conn.TransportTypeMqttUdp,
	}
	conn := &brokerAwareNotifyTestConn{
		sessionCloseTestConn: baseConn,
		online:               false, // 模拟处于 MQTT 离线 2 分钟宽限期内
	}

	cs := &ClientState{
		Ctx:       ctx,
		Cancel:    cancel,
		DeviceID:  "dev-mqtt-1",
		SessionID: "sess-1",
	}
	st := NewServerTransport(conn, cs)
	cm := &ChatManager{
		DeviceID:        "dev-mqtt-1",
		transport:       conn,
		clientState:     cs,
		serverTransport: st,
		ctx:             ctx,
		cancel:          cancel,
	}

	// 1. 离线宽限期内必须拒绝推送，不发送 QoS 0 指令
	if err := cm.SendNotify("http://192.168.1.10:8990/xiaozhi/notify/stream?token=abc", []NotifySubtitle{{StartMs: 0, Text: "测试"}}); err == nil {
		t.Fatal("expected SendNotify to fail when MQTT broker device is offline during grace period")
	}
	if len(baseConn.sentCmds) != 0 {
		t.Fatalf("expected 0 sent commands while offline, got %d", len(baseConn.sentCmds))
	}

	// 2. 设备恢复在线后应正常发送 notify 指令
	conn.online = true
	if err := cm.SendNotify("http://192.168.1.10:8990/xiaozhi/notify/stream?token=abc", []NotifySubtitle{{StartMs: 0, Text: "测试"}}); err != nil {
		t.Fatalf("expected SendNotify to succeed when online, got %v", err)
	}
	if len(baseConn.sentCmds) != 1 {
		t.Fatalf("expected 1 sent command, got %d", len(baseConn.sentCmds))
	}

	var sent map[string]interface{}
	if err := json.Unmarshal(baseConn.sentCmds[0], &sent); err != nil {
		t.Fatalf("unmarshal sent notify payload: %v", err)
	}
	if sent["type"] != "notify" || sent["audio_url"] != "http://192.168.1.10:8990/xiaozhi/notify/stream?token=abc" {
		t.Fatalf("unexpected notify payload: %+v", sent)
	}

	// 3. ServerTransport 关闭后必须拒绝推送
	_ = st.Close()
	if err := cm.SendNotify("http://192.168.1.10:8990/xiaozhi/notify/stream?token=abc", nil); err == nil {
		t.Fatal("expected SendNotify to fail after serverTransport is closed")
	}
}
