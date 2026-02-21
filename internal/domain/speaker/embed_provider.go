//go:build asr_server && cgo

package speaker

import (
	"context"
	"fmt"
	"sync"

	asr_core "voice_server/core"
	asr_server "voice_server/server"

	log "xiaozhi-esp32-server-golang/logger"
)

// EmbedProvider 通过进程内接口调用 asr_server 的声纹能力。
type EmbedProvider struct {
	engine    asr_core.Engine
	streaming asr_core.SpeakerStreamingSession
	threshold float32
	isActive  bool
	mutex     sync.Mutex
}

// NewEmbedProvider 创建进程内声纹识别提供者。
func NewEmbedProvider(config map[string]interface{}) (*EmbedProvider, error) {
	engine, err := asr_server.SharedEngineProvider().Engine()
	if err != nil {
		return nil, fmt.Errorf("初始化内嵌 asr_server 失败: %w", err)
	}
	if !engine.HasSpeakerService() {
		return nil, fmt.Errorf("speaker service 未初始化")
	}

	threshold := float32(0.6)
	if thresholdVal, ok := config["threshold"]; ok {
		switch v := thresholdVal.(type) {
		case float64:
			threshold = float32(v)
		case float32:
			threshold = v
		case int:
			threshold = float32(v)
		case int64:
			threshold = float32(v)
		}
		if threshold < 0 || threshold > 1 {
			log.Warnf("阈值 %.4f 超出有效范围 [0.0, 1.0]，使用默认值 0.6", threshold)
			threshold = 0.6
		}
	}

	return &EmbedProvider{
		engine:    engine,
		threshold: threshold,
	}, nil
}

// StartStreaming 启动流式识别会话。
func (p *EmbedProvider) StartStreaming(ctx context.Context, sampleRate int, agentId string) error {
	_ = ctx
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.isActive {
		return nil
	}

	streaming, err := p.engine.NewSpeakerStreamingSession("", agentId, "", "", sampleRate, p.threshold)
	if err != nil {
		log.Warnf("启动进程内声纹识别流失败: %v", err)
		return err
	}

	p.streaming = streaming
	p.isActive = true
	log.Debugf("进程内声纹识别流已启动，采样率: %d Hz, agent_id: %s, 阈值: %.4f", sampleRate, agentId, p.threshold)
	return nil
}

// SendAudioChunk 发送音频块。
func (p *EmbedProvider) SendAudioChunk(ctx context.Context, pcmData []float32) error {
	_ = ctx
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.isActive {
		return nil
	}
	if p.streaming == nil {
		p.isActive = false
		return fmt.Errorf("streaming session 未初始化")
	}

	if err := p.streaming.AcceptAudio(pcmData); err != nil {
		log.Warnf("发送音频块到进程内声纹识别失败: %v", err)
		p.streaming.Close()
		p.streaming = nil
		p.isActive = false
		return err
	}
	return nil
}

// FinishAndIdentify 完成识别并获取结果。
func (p *EmbedProvider) FinishAndIdentify(ctx context.Context) (*IdentifyResult, error) {
	_ = ctx
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.isActive {
		return nil, nil
	}
	if p.streaming == nil {
		p.isActive = false
		return nil, nil
	}

	result, err := p.streaming.FinishAndIdentify()
	p.streaming.Close()
	p.streaming = nil
	p.isActive = false
	if err != nil {
		log.Warnf("获取进程内声纹识别结果失败: %v", err)
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	return &IdentifyResult{
		Identified:  result.Identified,
		SpeakerID:   result.SpeakerID,
		SpeakerName: result.SpeakerName,
		Confidence:  result.Confidence,
		Threshold:   result.Threshold,
	}, nil
}

// Close 关闭提供者。
func (p *EmbedProvider) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.isActive = false
	if p.streaming != nil {
		p.streaming.Close()
		p.streaming = nil
	}
	return nil
}

// IsActive 返回当前激活状态。
func (p *EmbedProvider) IsActive() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.isActive
}
