package msg

import (
	"encoding/json"

	types_audio "xiaozhi-esp32-server-golang/internal/data/audio"
)

const (
	MDeviceMockPubTopicPrefix = "device-server"
	MDeviceMockSubTopicPrefix = "null"
	MDeviceSubTopicPrefix     = "/p2p/device_sub/"
	MDevicePubTopicPrefix     = "/p2p/device_public/"
	MDeviceLifecycleTopic     = MDevicePubTopicPrefix + "_server/lifecycle"
	MServerSubTopicPrefix     = "/p2p/device_public/#"
	MServerPubTopicPrefix     = MDeviceSubTopicPrefix
)

const (
	MqttLifecycleType         = "mqtt_lifecycle"
	MqttLifecycleStateOnline  = "online"
	MqttLifecycleStateOffline = "offline"
)

// 消息类型常量
const (
	MessageTypeHello      = "hello"       // 握手消息
	MessageTypeAbort      = "abort"       // 中止消息
	MessageTypeListen     = "listen"      // 监听消息
	MessageTypeIot        = "iot"         // 物联网消息
	MessageTypeMcp        = "mcp"         // MCP消息
	MessageTypeGoodBye    = "goodbye"     // 再见消息
	MessageTypeSpeakReady = "speak_ready" // 设备已准备好接收主动播报
)

// 服务器消息类型常量
const (
	ServerMessageTypeHello        = "hello"         // 握手消息
	ServerMessageTypeStt          = "stt"           // 语音转文本
	ServerMessageTypeTts          = "tts"           // 文本转语音
	ServerMessageTypeIot          = "iot"           // 物联网消息
	ServerMessageTypeLlm          = "llm"           // 大语言模型
	ServerMessageTypeText         = "text"          // 文本消息
	ServerMessageTypeGoodBye      = "goodbye"       // 再见消息
	ServerMessageTypeSpeakRequest = "speak_request" // 主动播报请求
	ServerMessageTypeNotify       = "notify"        // 异步单向语音通知
)

// 消息状态常量
const (
	MessageStateStart         = "start"          // 开始状态
	MessageStateSentenceStart = "sentence_start" // 句子开始状态
	MessageStateSentenceEnd   = "sentence_end"   // 句子结束状态
	MessageStateStop          = "stop"           // 停止状态
	MessageStateDetect        = "detect"         // 检测状态
	MessageStateAbort         = "abort"          // 中止状态
	MessageStateSuccess       = "success"        // 成功状态
	MessageStateReady         = "ready"          // 设备已准备好
)

type UdpConfig struct {
	Server string `json:"server"`
	Port   int    `json:"port"`
	Key    string `json:"key"`
	Nonce  string `json:"nonce"`
}

type MqttLifecycleEvent struct {
	Type     string `json:"type"`
	DeviceID string `json:"device_id"`
	State    string `json:"state"`
	ClientID string `json:"client_id,omitempty"`
	Ts       int64  `json:"ts"`
}

// NotifySubtitle 表示通知字幕
type NotifySubtitle struct {
	StartMs uint32 `json:"start_ms"`
	Text    string `json:"text"`
}

// ServerNotifyMessage 表示符合 PR #2191 规范的异步通知消息
type ServerNotifyMessage struct {
	Type      string           `json:"type"`
	AudioURL  string           `json:"audio_url"`
	Subtitles []NotifySubtitle `json:"subtitles,omitempty"`
}

// ServerMessage 表示服务器消息
type ServerMessage struct {
	Type        string                   `json:"type"`
	Text        string                   `json:"text,omitempty"`
	SessionID   string                   `json:"session_id,omitempty"`
	Version     int                      `json:"version"`
	State       string                   `json:"state,omitempty"`
	Transport   string                   `json:"transport,omitempty"`
	AudioFormat *types_audio.AudioFormat `json:"audio_params,omitempty"`
	Emotion     string                   `json:"emotion,omitempty"`
	AutoListen  *bool                    `json:"auto_listen,omitempty"`
	Udp         *UdpConfig               `json:"udp,omitempty"`
	PayLoad     json.RawMessage          `json:"payload,omitempty"`
}
