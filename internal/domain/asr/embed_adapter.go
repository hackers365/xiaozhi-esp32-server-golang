//go:build asr_server && cgo

package asr

import (
	"context"
	"fmt"
	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"

	"voice_server/core"
	"voice_server/server"
)

// AsrEmbedAdapter 仅做薄封装，底层实现复用 asr_server 的统一内嵌 ASR 服务。
type AsrEmbedAdapter struct {
	engine core.Engine
}

const embedMode = "offline"
const DefaultEmbedAsrConfigPath = "asr_server.json"

func NewAsrEmbedAdapter(_ map[string]interface{}) (AsrProvider, error) {
	engine, err := server.SharedEngineProvider().Engine()
	if err != nil {
		return nil, err
	}
	return &AsrEmbedAdapter{engine: engine}, nil
}

// InitAsrServerEmbed 统一初始化内嵌 ASR 共享依赖。
func InitAsrServerEmbed(configPath string) error {
	if configPath == "" {
		configPath = DefaultEmbedAsrConfigPath
	}

	log.Infof("正在预初始化内嵌 embed 引擎，配置文件: %s", configPath)

	if err := server.SharedEngineProvider().InitForEmbed(configPath); err != nil {
		return fmt.Errorf("embed 初始化失败: %w", err)
	}
	log.Info("asr_server embed 引擎预初始化完成")
	return nil
}

// IsAsrServerEmbedInitialized 判断内嵌 ASR 引擎是否已经初始化完成。
func IsAsrServerEmbedInitialized() bool {
	_, err := server.SharedEngineProvider().Engine()
	return err == nil
}

// RequireAsrServerEmbed 确保内嵌 ASR 已初始化；已初始化时直接返回。
func RequireAsrServerEmbed(configPath string) error {
	if IsAsrServerEmbedInitialized() {
		return nil
	}
	return InitAsrServerEmbed(configPath)
}

func (a *AsrEmbedAdapter) Process(pcmData []float32) (string, error) {
	if a.engine == nil {
		return "", fmt.Errorf("embed service 未初始化")
	}
	return a.engine.RecognizeFloat32(pcmData, audio.SampleRate)
}

func (a *AsrEmbedAdapter) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	if a.engine == nil {
		return nil, fmt.Errorf("embed service 未初始化")
	}
	if audioStream == nil {
		return nil, fmt.Errorf("audioStream 不能为空")
	}

	resultChan := make(chan types.StreamingResult, 1)
	go func() {
		defer close(resultChan)

		buffer := make([]float32, 0, audio.SampleRate*3)
		for {
			select {
			case <-ctx.Done():
				return
			case pcm, ok := <-audioStream:
				if !ok {
					text, err := a.engine.RecognizeWithVAD(buffer, audio.SampleRate)
					if err != nil {
						log.Warnf("embed 识别失败: %v", err)
						select {
						case resultChan <- types.StreamingResult{
							Error:   err,
							IsFinal: true,
							AsrType: constants.AsrTypeEmbed,
							Mode:    embedMode,
						}:
						case <-ctx.Done():
						}
						return
					}
					select {
					case resultChan <- types.StreamingResult{
						Text:    text,
						IsFinal: true,
						AsrType: constants.AsrTypeEmbed,
						Mode:    embedMode,
					}:
					case <-ctx.Done():
					}
					return
				}
				if len(pcm) > 0 {
					buffer = append(buffer, pcm...)
				}
			}
		}
	}()

	return resultChan, nil
}

func (a *AsrEmbedAdapter) Close() error {
	a.engine = nil
	return nil
}

func (a *AsrEmbedAdapter) IsValid() bool {
	return a != nil && a.engine != nil
}
