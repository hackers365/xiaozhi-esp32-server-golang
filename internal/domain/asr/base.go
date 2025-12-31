package asr

import (
	"context"
	"fmt"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

// Asr 语音识别接口
type AsrProvider interface {
	// Process 一次性处理整段音频，返回完整识别结果
	Process(pcmData []float32) (string, error)

	// StreamingRecognize 流式识别接口
	// 输入音频数据通过 audioStream 通道，识别结果通过返回的通道获取
	// 当 audioStream 被关闭时，表示输入结束，最终结果将会通过返回的通道发送，然后关闭该通道
	// 可以通过 ctx 控制识别过程的取消和超时
	StreamingRecognize(ctx context.Context, audioStream *schema.StreamReader[[]float32]) (chan types.StreamingResult, error)
}

// NewAsrProvider 创建一个新的ASR实例
// asrType: ASR引擎类型，目前支持 "funasr"
// config: ASR引擎配置，为 map[string]interface{} 类型
func NewAsrProvider(asrType string, config map[string]interface{}) (AsrProvider, error) {
	switch asrType {
	case constants.AsrTypeFunAsr:
		return NewFunasrAdapter(config)
	case constants.AsrTypeDoubao:
		log.Info("使用 豆包ASR 提供者")
		provider, err := doubao.NewDoubaoV2Adapter(config)
		if err != nil {
			log.Errorf("豆包ASR适配器创建失败: %v", err)
		} else {
			log.Info("豆包ASR适配器创建成功")
		}
		return provider, err
	default:
		return nil, fmt.Errorf("不支持的ASR引擎类型: %s，目前仅支持 'funasr'", asrType)
	}
}
