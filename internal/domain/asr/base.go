package asr

import (
	"context"
	"fmt"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// Asr 语音识别接口
type AsrProvider interface {
	// Process 一次性处理整段音频，返回完整识别结果
	Process(pcmData []float32) (string, error)

	// StreamingRecognize 流式识别接口
	// 输入音频数据通过 audioStream 通道，识别结果通过返回的通道获取
	// 当 audioStream 被关闭时，表示输入结束，最终结果将会通过返回的通道发送，然后关闭该通道
	// 可以通过 ctx 控制识别过程的取消和超时
	StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error)
	// Close 关闭资源，释放连接等
	Close() error
	// IsValid 检查资源是否有效
	IsValid() bool
}

// NewAsrProvider 创建一个新的ASR实例
// asrType: ASR引擎类型，目前支持 "funasr"
// config: ASR引擎配置，为 map[string]interface{} 类型
func NewAsrProvider(asrType string, config map[string]interface{}) (AsrProvider, error) {
	// 优先使用 config 中的 provider，否则使用参数中的 provider
	if configProvider, ok := config["provider"].(string); ok && configProvider != "" {
		asrType = configProvider
	}
	switch asrType {
	case constants.AsrTypeFunAsr:
		return NewFunasrAdapter(config)
	case constants.AsrTypeAliyunFunASR:
		return NewAliyunFunASRAdapter(config)
	case constants.AsrTypeDoubao:
		log.Info("使用 豆包ASR 提供者")
		provider, err := doubao.NewDoubaoV2Adapter(config)
		if err != nil {
			log.Errorf("豆包ASR适配器创建失败: %v", err)
		} else {
			log.Info("豆包ASR适配器创建成功")
		}
		return provider, err
	case constants.AsrTypeAliyunQwen3:
		log.Info("使用 阿里云 Qwen3 ASR 提供者")
		provider, err := NewAliyunQwen3Adapter(config)
		if err != nil {
			log.Errorf("阿里云 Qwen3 ASR 适配器创建失败: %v", err)
		} else {
			log.Info("阿里云 Qwen3 ASR 适配器创建成功")
		}
		return provider, err
	case constants.AsrTypeEmbed:
		log.Infof("使用 %s 本地直连 ASR 提供者", asrType)
		provider, err := NewAsrEmbedAdapter(config)
		if err != nil {
			log.Errorf("%s 适配器创建失败: %v", asrType, err)
		} else {
			log.Infof("%s 适配器创建成功", asrType)
		}
		return provider, err
	default:
		return nil, fmt.Errorf("不支持的ASR引擎类型: %s，目前仅支持 'funasr', 'aliyun_funasr', 'doubao', 'aliyun_qwen3', 'embed'", asrType)
	}
}
