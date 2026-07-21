package asr

import (
	"context"
	"strconv"

	"xiaozhi-esp32-server-golang/internal/domain/asr/local_asr_server"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// LocalAsrServerAdapter 将 local_asr_server 适配至 AsrProvider 接口
type LocalAsrServerAdapter struct {
	engine *local_asr_server.LocalAsrServer
}

// NewLocalAsrServerAdapter 创建 LocalAsrServerAdapter 实例
func NewLocalAsrServerAdapter(config map[string]interface{}) (AsrProvider, error) {
	cfg := parseLocalAsrServerConfig(config)
	log.Log().Infof("local_asr_server config: %+v", cfg)

	engine, err := local_asr_server.NewLocalAsrServer(cfg)
	if err != nil {
		return nil, err
	}
	return &LocalAsrServerAdapter{engine: engine}, nil
}

func parseLocalAsrServerConfig(config map[string]interface{}) local_asr_server.Config {
	cfg := local_asr_server.DefaultConfig

	// 兼容老格式嵌套，如 { local_asr_server: { ... } }
	if nested, ok := config["local_asr_server"].(map[string]interface{}); ok {
		config = nested
	} else if nested, ok := config["asr_server"].(map[string]interface{}); ok {
		config = nested
	}

	if host, ok := config["host"].(string); ok && host != "" {
		cfg.Host = host
	}
	if port, ok := config["port"].(string); ok && port != "" {
		cfg.Port = port
	} else if portInt, ok := config["port"].(int); ok && portInt > 0 {
		cfg.Port = strconv.Itoa(portInt)
	} else if portFloat, ok := config["port"].(float64); ok && portFloat > 0 {
		cfg.Port = strconv.Itoa(int(portFloat))
	}
	if wsURL, ok := config["ws_url"].(string); ok && wsURL != "" {
		cfg.WsURL = wsURL
	}
	if sampleRate, ok := config["sample_rate"].(int); ok && sampleRate > 0 {
		cfg.SampleRate = sampleRate
	} else if sampleRateFloat, ok := config["sample_rate"].(float64); ok && sampleRateFloat > 0 {
		cfg.SampleRate = int(sampleRateFloat)
	}
	if timeout, ok := config["timeout"].(int); ok && timeout > 0 {
		cfg.Timeout = timeout
	} else if timeoutFloat, ok := config["timeout"].(float64); ok && timeoutFloat > 0 {
		cfg.Timeout = int(timeoutFloat)
	}

	return cfg
}

// Process 实现 AsrProvider
func (a *LocalAsrServerAdapter) Process(pcmData []float32) (string, error) {
	return a.engine.Process(pcmData)
}

// StreamingRecognize 实现 AsrProvider
func (a *LocalAsrServerAdapter) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	return a.engine.StreamingRecognize(ctx, audioStream)
}

// Close 释放资源
func (a *LocalAsrServerAdapter) Close() error {
	return nil
}

// IsValid 检查适配器有效性
func (a *LocalAsrServerAdapter) IsValid() bool {
	return a != nil && a.engine != nil
}
