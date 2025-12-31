package asr

import (
	"context"
	"strconv"
	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/domain/asr/funasr"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

// FunasrAdapter 适配 funasr 包到 asr 接口
type FunasrAdapter struct {
	engine *funasr.Funasr
}

// NewFunasrAdapter 创建一个新的 FunASR 适配器
func NewFunasrAdapter(config map[string]interface{}) (AsrProvider, error) {
	// 创建 FunasrConfig 配置
	funasrConfig := funasr.FunasrConfig{
		Host:           "localhost",
		Port:           "10095",
		Mode:           "online",
		SampleRate:     audio.SampleRate,
		ChunkInterval:  audio.FrameDuration,
		MaxConnections: 5,
		Timeout:        30,
		AutoEnd:        false,
	}

	log.Log().Infof("funasr config: %+v", config)

	// 从 map 中获取配置项
	if host, ok := config["host"].(string); ok && host != "" {
		funasrConfig.Host = host
	}
	if port, ok := config["port"].(string); ok && port != "" {
		funasrConfig.Port = port
	} else if portInt, ok := config["port"].(int); ok && portInt > 0 {
		funasrConfig.Port = strconv.Itoa(portInt)
	} else if portFloat, ok := config["port"].(float64); ok && portFloat > 0 {
		funasrConfig.Port = strconv.Itoa(int(portFloat))
	}

	if mode, ok := config["mode"].(string); ok && mode != "" {
		funasrConfig.Mode = mode
	}
	if sampleRate, ok := config["sample_rate"].(int); ok && sampleRate > 0 {
		funasrConfig.SampleRate = sampleRate
	} else if sampleRateFloat, ok := config["sample_rate"].(float64); ok && sampleRateFloat > 0 {
		funasrConfig.SampleRate = int(sampleRateFloat)
	}
	if chunkInterval, ok := config["chunk_interval"].(int); ok && chunkInterval > 0 {
		funasrConfig.ChunkInterval = chunkInterval
	} else if chunkIntervalFloat, ok := config["chunk_interval"].(float64); ok && chunkIntervalFloat > 0 {
		funasrConfig.ChunkInterval = int(chunkIntervalFloat)
	}
	if maxConnections, ok := config["max_connections"].(int); ok && maxConnections > 0 {
		funasrConfig.MaxConnections = maxConnections
	} else if maxConnectionsFloat, ok := config["max_connections"].(float64); ok && maxConnectionsFloat > 0 {
		funasrConfig.MaxConnections = int(maxConnectionsFloat)
	}
	if timeout, ok := config["timeout"].(int); ok && timeout > 0 {
		funasrConfig.Timeout = timeout
	} else if timeoutFloat, ok := config["timeout"].(float64); ok && timeoutFloat > 0 {
		funasrConfig.Timeout = int(timeoutFloat)
	}
	if chunkSize, ok := config["chunk_size"].([]int); ok && len(chunkSize) > 0 {
		funasrConfig.ChunkSize = chunkSize
	}

	if autoEnd, ok := config["auto_end"].(bool); ok {
		funasrConfig.AutoEnd = autoEnd
	}

	// 创建FunASR引擎
	engine, err := funasr.NewFunasr(funasrConfig)
	if err != nil {
		return nil, err
	}
	return &FunasrAdapter{engine: engine}, nil
}

// Process 实现 Asr 接口
func (a *FunasrAdapter) Process(pcmData []float32) (string, error) {
	return a.engine.Process(pcmData)
}

// StreamingRecognize 实现流式识别接口
func (a *FunasrAdapter) StreamingRecognize(ctx context.Context, audioStream *schema.StreamReader[[]float32]) (chan types.StreamingResult, error) {
	return a.engine.StreamingRecognize(ctx, audioStream)
}
