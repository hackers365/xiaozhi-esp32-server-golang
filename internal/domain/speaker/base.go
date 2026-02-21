package speaker

import (
	"context"
	"strings"

	log "xiaozhi-esp32-server-golang/logger"
)

// SpeakerProvider 声纹识别提供者接口
type SpeakerProvider interface {
	// StartStreaming 启动流式识别
	StartStreaming(ctx context.Context, sampleRate int, agentId string) error

	// SendAudioChunk 发送音频数据块
	SendAudioChunk(ctx context.Context, audioData []float32) error

	// FinishAndIdentify 完成输入并获取识别结果
	FinishAndIdentify(ctx context.Context) (*IdentifyResult, error)

	// IsActive 检查是否处于激活状态
	IsActive() bool

	// Close 关闭连接
	Close() error
}

// GetSpeakerProvider 获取声纹识别提供者
func GetSpeakerProvider(config map[string]interface{}) (SpeakerProvider, error) {
	serviceMode := ""
	if mode, ok := config["service"].(string); ok && mode != "" {
		serviceMode = strings.ToLower(strings.TrimSpace(mode))
	}
	if serviceMode == "" {
		if mode, ok := config["mode"].(string); ok && mode != "" {
			serviceMode = strings.ToLower(strings.TrimSpace(mode))
		}
	}

	if serviceMode == "embed" {
		log.Infof("使用 embed 本地直连声纹提供者")
		return NewEmbedProvider(config)
	}

	return NewAsrServerProvider(config)
}
