package doubao

import (
	"context"
	"fmt"

	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

// DoubaoV2Adapter 适配器，实现现有的AsrProvider接口
type DoubaoV2Adapter struct {
	engine *DoubaoV2ASR
}

// NewDoubaoV2Adapter 创建一个新的豆包ASR适配器
func NewDoubaoV2Adapter(config map[string]interface{}) (*DoubaoV2Adapter, error) {

	// 创建豆包ASR配置
	doubaoConfig := DoubaoV2Config{}

	// 从map中获取配置项
	if appID, ok := config["appid"].(string); ok && appID != "" {
		doubaoConfig.AppID = appID
	}
	if accessToken, ok := config["access_token"].(string); ok && accessToken != "" {
		doubaoConfig.AccessToken = accessToken
	}
	if wsURL, ok := config["ws_url"].(string); ok && wsURL != "" {
		doubaoConfig.WsURL = wsURL
	}
	if modelName, ok := config["model_name"].(string); ok && modelName != "" {
		doubaoConfig.ModelName = modelName
	}
	if endWindowSize, ok := config["end_window_size"].(int); ok && endWindowSize > 0 {
		doubaoConfig.EndWindowSize = endWindowSize
	} else if endWindowSizeFloat, ok := config["end_window_size"].(float64); ok && endWindowSizeFloat > 0 {
		doubaoConfig.EndWindowSize = int(endWindowSizeFloat)
	}
	if enablePunc, ok := config["enable_punc"].(bool); ok {
		doubaoConfig.EnablePunc = enablePunc
	}
	if enableITN, ok := config["enable_itn"].(bool); ok {
		doubaoConfig.EnableITN = enableITN
	}
	if enableDDC, ok := config["enable_ddc"].(bool); ok {
		doubaoConfig.EnableDDC = enableDDC
	}
	if chunkDuration, ok := config["chunk_duration"].(int); ok && chunkDuration > 0 {
		doubaoConfig.ChunkDuration = chunkDuration
	} else if chunkDurationFloat, ok := config["chunk_duration"].(float64); ok && chunkDurationFloat > 0 {
		doubaoConfig.ChunkDuration = int(chunkDurationFloat)
	}
	if timeout, ok := config["timeout"].(int); ok && timeout > 0 {
		doubaoConfig.Timeout = timeout
	} else if timeoutFloat, ok := config["timeout"].(float64); ok && timeoutFloat > 0 {
		doubaoConfig.Timeout = int(timeoutFloat)
	}

	// 创建豆包ASR引擎
	engine, err := NewDoubaoV2ASR(doubaoConfig)
	if err != nil {
		log.Errorf("创建豆包ASR引擎失败: %v", err)
		return nil, fmt.Errorf("创建豆包ASR引擎失败: %v", err)
	}
	log.Info("豆包ASR引擎创建成功")

	return &DoubaoV2Adapter{
		engine: engine,
	}, nil
}

// Process 实现一次性处理整段音频，返回完整识别结果
func (d *DoubaoV2Adapter) Process(pcmData []float32) (string, error) {
	return "", nil
}

// StreamingRecognize 实现流式识别接口
func (d *DoubaoV2Adapter) StreamingRecognize(ctx context.Context, audioStream *schema.StreamReader[[]float32]) (chan types.StreamingResult, error) {
	return d.engine.StreamingRecognize(ctx, audioStream)
}
