package notify

import "xiaozhi-esp32-server-golang/internal/data/msg"

// NotifyTokenPayload 仅在 URL Token 中加密传输的精简数据
type NotifyTokenPayload struct {
	UUID      string `json:"u"` // 全局唯一标识符
	ExpiresAt int64  `json:"e"` // 过期时间戳 (Unix秒)
	Nonce     string `json:"n"` // 随机串
}

// NotifyConfig 存储在 Redis 中的完整通知配置 (Key: xiaozhi:notify:{uuid})
type NotifyConfig struct {
	UUID          string               `json:"uuid"`
	DeviceID      string               `json:"device_id"`
	Text          string               `json:"text"`
	Voice         string               `json:"voice,omitempty"`
	TTSConfigID   string               `json:"tts_config_id,omitempty"`
	Speed         float64              `json:"speed,omitempty"`
	SampleRate    int                  `json:"sample_rate,omitempty"`
	FrameDuration int                  `json:"frame_duration,omitempty"`
	Subtitles     []msg.NotifySubtitle `json:"subtitles,omitempty"`
	CreatedAt     int64                `json:"created_at"`
}

// PrepareNotifyRequest 准备通知的入参
type PrepareNotifyRequest struct {
	DeviceID    string               `json:"device_id"`
	Text        string               `json:"text"`
	Voice       string               `json:"voice,omitempty"`
	TTSConfigID string               `json:"tts_config_id,omitempty"`
	Speed       float64              `json:"speed,omitempty"`
	AudioURL    string               `json:"audio_url,omitempty"`
	Subtitles   []msg.NotifySubtitle `json:"subtitles,omitempty"`
}

// PrepareNotifyResult 准备通知的返回结果
type PrepareNotifyResult struct {
	UUID      string               `json:"uuid"`
	AudioURL  string               `json:"audio_url"`
	Subtitles []msg.NotifySubtitle `json:"subtitles"`
}
