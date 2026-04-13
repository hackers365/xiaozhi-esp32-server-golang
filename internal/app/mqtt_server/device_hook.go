package mqtt_server

import (
	"fmt"
	"strings"
	"time"

	mqttServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	client "xiaozhi-esp32-server-golang/internal/data/msg"
	log "xiaozhi-esp32-server-golang/logger"
)

// DeviceHook 设备权限与自动订阅钩子
// 普通用户禁止显式订阅，只允许发布指定 topic，连接时自动订阅 /devices/p2p/{mac}
type DeviceHook struct {
	mqttServer.HookBase
	server *mqttServer.Server
}

func (h *DeviceHook) ID() string {
	return "custom-device-hook"
}

func (h *DeviceHook) Provides(b byte) bool {
	return b == mqttServer.OnDisconnect || b == mqttServer.OnACLCheck || b == mqttServer.OnSessionEstablished || b == mqttServer.OnSubscribe || b == mqttServer.OnPublish
}

// OnACLCheck 发布/订阅权限控制
func (h *DeviceHook) OnACLCheck(cl *mqttServer.Client, topic string, write bool) bool {
	isAdmin := isAdminUser(cl)

	if isAdmin {
		return true // 超级管理员无限制
	}

	if write {
		// 只允许普通用户发布到 "device-server"
		if topic == client.MDeviceMockPubTopicPrefix {
			return true
		}
		log.Warnf("禁止普通用户发布到 %s", topic)
		return false
	}
	// 禁止显式订阅
	//return false
	return true
}

func (h *DeviceHook) OnConnect(cl *mqttServer.Client, pk packets.Packet) error {
	isAdmin := isAdminUser(cl)
	if isAdmin {
		return nil
	}
	pk.Connect.Clean = true
	return nil
}

func (h *DeviceHook) OnDisconnect(cl *mqttServer.Client, err error, ok bool) {
	isAdmin := isAdminUser(cl)
	if isAdmin {
		return
	}
	if cl.IsTakenOver() {
		log.Infof("客户端 %s 已被同ID新连接接管，跳过取消订阅", cl.ID)
		return
	}
	mac := parseMacFromClientId(cl.ID)
	if mac == "" {
		log.Info("警告: 无法从客户端ID解析MAC地址:", cl.ID)
		return
	}
	topic := fmt.Sprintf("%s%s", client.MDeviceSubTopicPrefix, mac)

	action := h.server.Topics.Unsubscribe(topic, cl.ID)
	log.Infof("取消订阅客户端 %s 到主题 %s, action: %v", cl.ID, topic, action)

	return
}

// OnSessionEstablished 连接建立后自动订阅
func (h *DeviceHook) OnSessionEstablished(cl *mqttServer.Client, pk packets.Packet) {
	isAdmin := isAdminUser(cl)
	mac := parseMacFromClientId(cl.ID)
	if isAdmin {
		return // 超级管理员不做限制
	}
	if mac == "" {
		log.Info("警告: 无法从客户端ID解析MAC地址:", cl.ID)
		return
	}

	topic := fmt.Sprintf("%s%s", client.MDeviceSubTopicPrefix, mac)

	// 使用服务器的API直接订阅，而不是注入数据包
	clientID := cl.ID
	exists := h.server.Topics.Subscribe(clientID, packets.Subscription{
		Filter: topic,
		Qos:    0,
	})

	log.Infof("订阅客户端 %s 到主题 %s, exists: %v", clientID, topic, exists)
}

// OnSubscribe 打印订阅包
func (h *DeviceHook) OnSubscribe(cl *mqttServer.Client, pk packets.Packet) packets.Packet {
	log.Info("=== 收到订阅包 ===")
	log.Infof("客户端ID: %s", cl.ID)
	log.Infof("包类型: %v", pk.FixedHeader.Type)
	log.Infof("包ID: %d", pk.PacketID)

	if len(pk.Filters) > 0 {
		log.Info("订阅信息:")
		for i, sub := range pk.Filters {
			log.Infof("  %d. 主题: %s, QoS: %d", i+1, sub.Filter, sub.Qos)
		}
	}

	log.Info("==================")
	return pk
}

// OnPublish 打印发布包
func (h *DeviceHook) OnPublish(cl *mqttServer.Client, pk packets.Packet) (packets.Packet, error) {
	log.Info("=== 收到发布包 ===")
	log.Infof("客户端ID: %s", cl.ID)
	log.Infof("包类型: %v", pk.FixedHeader.Type)
	log.Infof("包ID: %d", pk.PacketID)
	log.Infof("主题: %s", pk.TopicName)

	if isAdminUser(cl) {
		return pk, nil
	}

	if len(pk.Payload) > 0 {
		if len(pk.Payload) > 100 {
			// 如果消息太长，只显示前100个字节
			log.Infof("消息内容(前100字节): %s...", pk.Payload[:100])
		} else {
			log.Infof("消息内容: %s", pk.Payload)
		}
	} else {
		log.Info("消息内容: <空>")
	}

	// 服务端 inline client 下行到设备订阅主题时，不做主题改写
	if strings.HasPrefix(pk.TopicName, client.MDeviceSubTopicPrefix) {
		log.Info("==================")
		return pk, nil
	}

	// 设备上行消息转发到统一设备公共主题
	mac := parseMacFromClientId(cl.ID)
	if mac == "" {
		log.Infof("跳过主题改写: 无法从客户端ID解析MAC地址: %s", cl.ID)
		log.Info("==================")
		return pk, nil
	}
	forwardTopic := fmt.Sprintf("%s%s", client.MDevicePubTopicPrefix, mac)
	DispatchMcpPublishResponse(forwardTopic, pk.Payload)

	pk.TopicName = forwardTopic

	log.Info("==================")
	return pk, nil
}

// 判断是否超级管理员
func isAdminUser(cl *mqttServer.Client) bool {
	return string(cl.Properties.Username) == "admin"
}

// 解析 clientId，获取 mac 地址
func parseMacFromClientId(clientId string) string {
	parts := strings.Split(clientId, "@@@")
	if len(parts) >= 3 {
		return parts[1]
	}
	return ""
}

// 启动周期性打印订阅主题的任务
func (h *DeviceHook) StartPeriodicSubscriptionPrinter(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			h.PrintAllClientSubscriptions()
		}
	}()
}

// 打印所有客户端的订阅主题
func (h *DeviceHook) PrintAllClientSubscriptions() {
	log.Info("=== 客户端订阅主题列表 ===")
	clients := h.server.Clients.GetAll()
	if len(clients) == 0 {
		log.Info("当前无连接客户端")
		return
	}

	for clientID, _ := range clients {
		log.Infof("客户端 %s 订阅的主题: ", clientID)

		// 使用server.Topics.Subscribers("+")获取所有主题的订阅者
		// 然后过滤出与当前clientID匹配的订阅
		allSubs := h.server.Topics.Subscribers("+")
		foundTopics := false

		// 检查客户端的订阅
		if subs, ok := allSubs.Subscriptions[clientID]; ok {
			log.Infof("  - %s (QoS: %d)", subs.Filter, subs.Qos)
			foundTopics = true
		}

		// 检查更多可能的主题订阅
		allSubs = h.server.Topics.Subscribers("#")
		if subs, ok := allSubs.Subscriptions[clientID]; ok {
			log.Infof("  - %s (QoS: %d)", subs.Filter, subs.Qos)
			foundTopics = true
		}

		// 再检查一下特定主题
		mac := parseMacFromClientId(clientID)
		if mac != "" {
			topic := "/devices/p2p/" + mac
			topicSubs := h.server.Topics.Subscribers(topic)
			if subs, ok := topicSubs.Subscriptions[clientID]; ok {
				log.Infof("  - %s (QoS: %d)", subs.Filter, subs.Qos)
				foundTopics = true
			}
		}

		if !foundTopics {
			log.Info("  无订阅主题或无法获取")
		}
	}
	log.Info("=====================")
}
