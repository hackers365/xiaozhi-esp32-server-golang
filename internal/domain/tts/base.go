package tts

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/tts/cosyvoice"
	"xiaozhi-esp32-server-golang/internal/domain/tts/doubao"
	"xiaozhi-esp32-server-golang/internal/domain/tts/edge"
	"xiaozhi-esp32-server-golang/internal/domain/tts/edge_offline"
	"xiaozhi-esp32-server-golang/internal/domain/tts/minimax"
	"xiaozhi-esp32-server-golang/internal/domain/tts/mimo"
	"xiaozhi-esp32-server-golang/internal/domain/tts/openai"
	"xiaozhi-esp32-server-golang/internal/domain/tts/qwen"
	"xiaozhi-esp32-server-golang/internal/domain/tts/streaming"
	"xiaozhi-esp32-server-golang/internal/domain/tts/xiaozhi"
	"xiaozhi-esp32-server-golang/internal/domain/tts/xunfei"
	"xiaozhi-esp32-server-golang/internal/domain/tts/xunfei_super_tts"
	"xiaozhi-esp32-server-golang/internal/domain/tts/zhipu"
)

// 基础TTS提供者接口（不含Context方法）
type BaseTTSProvider interface {
	TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error)
	TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error)
}

// DualStreamProvider TTS 输入与输出均为流式的可选接口：边收文本边合成输出。Provider 若支持则实现此接口。
type DualStreamProvider interface {
	StreamingSynthesize(ctx context.Context, textChan <-chan string, sampleRate int, channels int, frameDuration int) (outputChan chan streaming.SynthesisEvent, err error)
}

// 完整TTS提供者接口（包含Context方法）
type TTSProvider interface {
	BaseTTSProvider
	// SetVoice 动态设置音色参数
	// voiceConfig: 包含音色相关配置的 map，如 {"voice": "xxx"} 或 {"spk_id": "xxx"}
	SetVoice(voiceConfig map[string]interface{}) error
	// Close 关闭资源，释放连接等
	Close() error
	// IsValid 检查资源是否有效（连接是否存活等）
	IsValid() bool
}

// GetTTSProvider 获取一个完整的TTS提供者（支持Context）
// providerName: 可能是 config_id/provider 或资源池 key（如 "edge_tts:zh-CN-XiaoxiaoNeural"）
// config: 从数据库configs表的json_data字段解析的配置map
// 优先使用 config 中的 provider 字段，否则从 providerName 解析（取 ":" 前部分）
func GetTTSProvider(providerName string, config map[string]interface{}) (TTSProvider, error) {
	effectiveName := providerName
	if configProvider, ok := config["provider"].(string); ok && configProvider != "" {
		effectiveName = configProvider
	}
	// 资源池 key 格式为 "provider:voiceID"，取前半部分作为提供者类型
	if idx := strings.Index(effectiveName, ":"); idx > 0 {
		effectiveName = effectiveName[:idx]
	}
	var baseProvider BaseTTSProvider

	switch effectiveName {
	case constants.TtsTypeDoubao:
		baseProvider = doubao.NewDoubaoTTSProvider(config)
	case constants.TtsTypeDoubaoWS:
		baseProvider = doubao.NewDoubaoWSProvider(config)
	case constants.TtsTypeCosyvoice:
		baseProvider = cosyvoice.NewCosyVoiceTTSProvider(config)
	case constants.TtsTypeEdge:
		baseProvider = edge.NewEdgeTTSProvider(config)
	case constants.TtsTypeEdgeOffline:
		baseProvider = edge_offline.NewEdgeOfflineTTSProvider(config)
	case constants.TtsTypeXiaozhi:
		baseProvider = xiaozhi.NewXiaozhiProvider(config)
	case constants.TtsTypeXunfei:
		baseProvider = xunfei.NewXunfeiTTSProvider(config)
	case constants.TtsTypeXunfeiSuper:
		baseProvider = xunfei_super_tts.NewXunfeiSuperTTSProvider(config)
	case constants.TtsTypeOpenAI:
		baseProvider = openai.NewOpenAITTSProvider(config)
	case constants.TtsTypeZhipu:
		baseProvider = zhipu.NewZhipuTTSProvider(config)
	case constants.TtsTypeMinimax:
		baseProvider = minimax.NewMinimaxTTSProvider(config)
	case constants.TtsTypeAliyunQwen:
		baseProvider = qwen.NewQwenTTSProvider(config)
	case constants.TtsTypeMiMo:
		provider := mimo.NewProvider(config)
		if provider == nil {
			return nil, fmt.Errorf("MiMo TTS 初始化失败，请检查 API Key 和 API URL")
		}
		baseProvider = provider
	case constants.TtsTypeIndexTTSVLLM:
		baseProvider = openai.NewOpenAITTSProvider(buildIndexTTSOpenAIConfig(config))
	default:
		return nil, fmt.Errorf("不支持的TTS提供者: %s", effectiveName)
	}

	if baseProvider == nil {
		return nil, fmt.Errorf("无法创建TTS提供者: %s", effectiveName)
	}

	// 使用适配器包装基础提供者，转换为完整的TTSProvider
	provider := &ContextTTSAdapter{baseProvider}

	return provider, nil
}

func buildIndexTTSOpenAIConfig(config map[string]interface{}) map[string]interface{} {
	const (
		defaultIndexTTSURL   = "http://127.0.0.1:7860/audio/speech"
		defaultIndexTTSModel = "indextts-vllm"
	)

	normalized := make(map[string]interface{}, len(config)+4)
	for k, v := range config {
		normalized[k] = v
	}

	apiURL, _ := normalized["api_url"].(string)
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		apiURL = defaultIndexTTSURL
	} else {
		parsed, err := url.Parse(apiURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			trimmed := strings.TrimRight(apiURL, "/")
			if !strings.HasSuffix(strings.ToLower(trimmed), "/audio/speech") {
				trimmed += "/audio/speech"
			}
			apiURL = trimmed
		} else {
			if strings.TrimSpace(parsed.Path) == "" || parsed.Path == "/" {
				parsed.Path = "/audio/speech"
				parsed.RawPath = ""
				apiURL = parsed.String()
			}
		}
	}
	normalized["api_url"] = strings.TrimRight(apiURL, "/")

	if model, _ := normalized["model"].(string); strings.TrimSpace(model) == "" {
		normalized["model"] = defaultIndexTTSModel
	}
	if responseFormat, _ := normalized["response_format"].(string); strings.TrimSpace(responseFormat) == "" {
		normalized["response_format"] = "wav"
	}
	if _, exists := normalized["stream"]; !exists {
		normalized["stream"] = false
	}
	if _, exists := normalized["speed"]; !exists {
		normalized["speed"] = float64(1.0)
	}

	return normalized
}

// ContextTTSAdapter 是一个适配器，为基础TTS提供者添加Context支持
type ContextTTSAdapter struct {
	Provider BaseTTSProvider
}

// StreamingSynthesize 代理到原始提供者的双流式合成接口
func (a *ContextTTSAdapter) StreamingSynthesize(ctx context.Context, textChan <-chan string, sampleRate int, channels int, frameDuration int) (outputChan chan streaming.SynthesisEvent, err error) {
	// 检查底层 Provider 是否支持双流式
	if dsProvider, ok := a.Provider.(DualStreamProvider); ok {
		return dsProvider.StreamingSynthesize(ctx, textChan, sampleRate, channels, frameDuration)
	}
	return nil, fmt.Errorf("底层 Provider 不支持双流式合成")
}

// TextToSpeech 代理到原始提供者
func (a *ContextTTSAdapter) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	return a.Provider.TextToSpeech(ctx, text, sampleRate, channels, frameDuration)
}

// TextToSpeechStream 代理到原始提供者
func (a *ContextTTSAdapter) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	return a.Provider.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
}

// SetVoice 代理到底层 Provider 的 SetVoice 方法
func (a *ContextTTSAdapter) SetVoice(voiceConfig map[string]interface{}) error {
	// 如果底层 Provider 实现了 SetVoice 方法，直接调用
	if setter, ok := a.Provider.(interface {
		SetVoice(map[string]interface{}) error
	}); ok {
		return setter.SetVoice(voiceConfig)
	}
	// 否则返回不支持的错误
	return fmt.Errorf("底层 Provider 不支持 SetVoice 方法")
}

// TextToSpeechWithContext 使用Context版本的文本转语音
func (a *ContextTTSAdapter) TextToSpeechWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// 检查提供者是否直接支持Context版本
	if provider, ok := a.Provider.(interface {
		TextToSpeechWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error)
	}); ok {
		// 提供者直接支持Context版本
		return provider.TextToSpeechWithContext(ctx, text, sampleRate, channels, frameDuration)
	}

	// 否则使用标准版本，并通过goroutine和channel实现上下文控制
	resultChan := make(chan struct {
		frames [][]byte
		err    error
	})

	go func() {
		frames, err := a.Provider.TextToSpeech(ctx, text, sampleRate, channels, frameDuration)
		select {
		case <-ctx.Done():
			// 上下文已取消，不发送结果
			return
		case resultChan <- struct {
			frames [][]byte
			err    error
		}{frames, err}:
			// 结果已发送
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultChan:
		return result.frames, result.err
	}
}

// TextToSpeechStreamWithContext 使用Context版本的流式文本转语音
func (a *ContextTTSAdapter) TextToSpeechStreamWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, cancelFunc func(), err error) {
	// 检查提供者是否直接支持Context版本
	if provider, ok := a.Provider.(interface {
		TextToSpeechStreamWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, func(), error)
	}); ok {
		// 提供者直接支持Context版本
		return provider.TextToSpeechStreamWithContext(ctx, text, sampleRate, channels, frameDuration)
	}

	// 否则使用标准版本，但创建一个包装器来处理上下文取消
	streamCtx, cancel := context.WithCancel(ctx)
	streamChan, err := a.Provider.TextToSpeechStream(streamCtx, text, sampleRate, channels, frameDuration)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	cancelFunc = cancel

	// 创建一个新的输出通道，用于转发和处理取消
	outputChan = make(chan []byte, 10)

	// 创建一个goroutine来转发数据并监听上下文取消
	go func() {
		defer close(outputChan)

		for {
			select {
			case <-streamCtx.Done():
				// 上下文已取消，调用原始取消函数并退出
				cancelFunc()
				return
			case frame, ok := <-streamChan:
				if !ok {
					// 原始通道已关闭
					return
				}
				// 转发数据
				select {
				case <-streamCtx.Done():
					// 上下文已取消
					cancelFunc()
					return
				case outputChan <- frame:
					// 成功转发数据
				}
			}
		}
	}()

	return outputChan, cancelFunc, nil
}

// Close 关闭资源
func (a *ContextTTSAdapter) Close() error {
	// 如果底层 Provider 实现了 Close 方法，直接调用
	if closer, ok := a.Provider.(interface {
		Close() error
	}); ok {
		return closer.Close()
	}
	return nil
}

// IsValid 检查资源是否有效
func (a *ContextTTSAdapter) IsValid() bool {
	// 如果底层 Provider 实现了 IsValid 方法，直接调用
	if validator, ok := a.Provider.(interface {
		IsValid() bool
	}); ok {
		return validator.IsValid()
	}
	// 否则检查 Provider 是否为 nil
	return a.Provider != nil
}
