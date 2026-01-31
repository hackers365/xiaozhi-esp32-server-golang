package microsoft

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	sdkAudio "github.com/Microsoft/cognitive-services-speech-sdk-go/audio"
	"github.com/Microsoft/cognitive-services-speech-sdk-go/common"
	"github.com/Microsoft/cognitive-services-speech-sdk-go/speech"

	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
)

// 全局连接池：复用 SpeechConfig 对象
var (
	configPool     = make(map[string]*speech.SpeechConfig)
	configPoolLock sync.RWMutex
)

// MicrosoftASRProvider Microsoft ASR 提供者
// 支持流式识别，输入为 []float32 PCM 数据，输出识别文本
// 配置参数：subscription_key, region, language, timeout
type MicrosoftASRProvider struct {
	SubscriptionKey string
	Region          string
	Language        string
	Timeout         int
	SampleRate      int // 固定为 16000
}

// NewMicrosoftASRProvider 创建新的 Microsoft ASR Provider
func NewMicrosoftASRProvider(config map[string]interface{}) (*MicrosoftASRProvider, error) {
	subscriptionKey, _ := config["subscription_key"].(string)
	region, _ := config["region"].(string)
	language, _ := config["language"].(string)
	timeout, _ := config["timeout"].(int)
	sampleRate, _ := config["sample_rate"].(int)

	// 设置默认值
	if language == "" {
		language = "zh-CN"
	}
	if timeout == 0 {
		timeout = 60
	}
	if sampleRate == 0 {
		sampleRate = 16000 // 固定为 16kHz
	}

	if subscriptionKey == "" {
		return nil, fmt.Errorf("缺少 subscription_key 配置")
	}
	if region == "" {
		return nil, fmt.Errorf("缺少 region 配置")
	}

	return &MicrosoftASRProvider{
		SubscriptionKey: subscriptionKey,
		Region:          region,
		Language:        language,
		Timeout:         timeout,
		SampleRate:      16000, // 固定为 16kHz
	}, nil
}

// Close 关闭资源。Microsoft ASR 使用全局配置池，实例本身无长连接，故为空实现。
func (p *MicrosoftASRProvider) Close() error {
	return nil
}

// IsValid 检查配置是否有效（必填项非空）。
func (p *MicrosoftASRProvider) IsValid() bool {
	return p != nil && p.SubscriptionKey != "" && p.Region != ""
}

// getOrCreateConfig 获取或创建 SpeechConfig（复用连接池）
func getOrCreateConfig(subscriptionKey, region, language string) (*speech.SpeechConfig, error) {
	key := fmt.Sprintf("%s_%s_%s", subscriptionKey, region, language)

	configPoolLock.RLock()
	config, exists := configPool[key]
	configPoolLock.RUnlock()

	if exists && config != nil {
		log.Debugf("复用 Microsoft ASR SpeechConfig, key: %s", key)
		return config, nil
	}

	// 创建新的配置
	configPoolLock.Lock()
	defer configPoolLock.Unlock()

	// 双重检查
	if config, exists = configPool[key]; exists {
		return config, nil
	}

	config, err := speech.NewSpeechConfigFromSubscription(subscriptionKey, region)
	if err != nil {
		return nil, fmt.Errorf("创建 SpeechConfig 失败: %v", err)
	}

	err = config.SetSpeechRecognitionLanguage(language)
	if err != nil {
		config.Close()
		return nil, fmt.Errorf("设置识别语言失败: %v", err)
	}

	configPool[key] = config
	log.Debugf("创建新的 Microsoft ASR SpeechConfig, key: %s", key)
	return config, nil
}

// float32ToPCM16Bytes 将 float32 PCM 数据转换为 16-bit PCM bytes
func float32ToPCM16Bytes(samples []float32) []byte {
	pcmBytes := make([]byte, len(samples)*2)
	for i, sample := range samples {
		// 将 float32 (-1.0 到 1.0) 转换为 int16 (-32768 到 32767)
		var intSample int16
		if sample > 1.0 {
			intSample = 32767
		} else if sample < -1.0 {
			intSample = -32768
		} else {
			intSample = int16(sample * 32767)
		}
		// 小端序写入字节数组
		binary.LittleEndian.PutUint16(pcmBytes[i*2:], uint16(intSample))
	}
	return pcmBytes
}

// Process 一次性处理整段音频（通过流式识别实现）
func (p *MicrosoftASRProvider) Process(pcmData []float32) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.Timeout)*time.Second)
	defer cancel()

	// 创建一个临时的 channel
	audioStream := make(chan []float32, 100)

	// 在 goroutine 中发送所有数据
	go func() {
		defer close(audioStream)
		// 将数据分成小块发送
		chunkSize := 1600 // 100ms 的音频数据 (16kHz * 0.1s = 1600 samples)
		for i := 0; i < len(pcmData); i += chunkSize {
			end := i + chunkSize
			if end > len(pcmData) {
				end = len(pcmData)
			}
			chunk := pcmData[i:end]
			select {
			case <-ctx.Done():
				return
			case audioStream <- chunk:
			}
		}
	}()

	// 调用流式识别
	resultChan, err := p.StreamingRecognize(ctx, audioStream)
	if err != nil {
		return "", err
	}

	// 收集所有结果，返回最后一个最终结果
	var finalText string
	for result := range resultChan {
		if result.Error != nil {
			return "", result.Error
		}
		if result.Text != "" {
			finalText = result.Text
			if result.IsFinal {
				break
			}
		}
	}

	return finalText, nil
}

// StreamingRecognize 流式识别接口
func (p *MicrosoftASRProvider) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	startTs := time.Now().UnixMilli()

	// 从连接池获取或创建 SpeechConfig
	config, err := getOrCreateConfig(p.SubscriptionKey, p.Region, p.Language)
	if err != nil {
		return nil, fmt.Errorf("获取 SpeechConfig 失败: %v", err)
	}

	// 创建音频流（使用默认格式：16kHz, 16bit, mono PCM）
	format, err := sdkAudio.GetDefaultInputFormat()
	if err != nil {
		return nil, fmt.Errorf("创建音频格式失败: %v", err)
	}
	defer format.Close()

	stream, err := sdkAudio.CreatePushAudioInputStreamFromFormat(format)
	if err != nil {
		return nil, fmt.Errorf("创建音频流失败: %v", err)
	}

	audioConfig, err := sdkAudio.NewAudioConfigFromStreamInput(stream)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("创建音频配置失败: %v", err)
	}
	defer audioConfig.Close()

	// 创建语音识别器
	recognizer, err := speech.NewSpeechRecognizerFromConfig(config, audioConfig)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("创建识别器失败: %v", err)
	}

	// 创建结果通道
	resultChan := make(chan types.StreamingResult, 10)

	// 使用 sync.Once 确保通道只关闭一次
	var closeOnce sync.Once
	closeResultChan := func() {
		closeOnce.Do(func() {
			close(resultChan)
		})
	}

	// 设置事件回调
	recognizer.SessionStarted(func(event speech.SessionEventArgs) {
		defer event.Close()
		log.Debugf("Microsoft ASR 会话开始, SessionID: %s", event.SessionID)
	})

	// 用于标记识别会话是否已停止
	sessionStopped := make(chan struct{})

	recognizer.SessionStopped(func(event speech.SessionEventArgs) {
		defer event.Close()
		log.Debugf("Microsoft ASR 会话结束, SessionID: %s", event.SessionID)
		select {
		case <-sessionStopped:
			// 已经关闭，不需要再次关闭
		default:
			close(sessionStopped)
		}
	})

	// Recognizing 事件：中间结果（仅记录日志，不发送到结果通道）
	recognizer.Recognizing(func(event speech.SpeechRecognitionEventArgs) {
		defer event.Close()
		if event.Result.Text != "" {
			//log.Debugf("Microsoft ASR 中间结果: %s (不输出)", event.Result.Text)
			// 不发送中间结果，只输出最终结果
		}
	})

	// Recognized 事件：最终结果
	recognizer.Recognized(func(event speech.SpeechRecognitionEventArgs) {
		defer event.Close()
		log.Infof("Microsoft ASR Recognized 事件, Reason: %v, Text: '%s'", event.Result.Reason, event.Result.Text)
		if event.Result.Reason == common.RecognizedSpeech {
			select {
			case <-ctx.Done():
				log.Warnf("Microsoft ASR 上下文已取消，无法发送最终结果")
				return
			case resultChan <- types.StreamingResult{
				Text:    event.Result.Text,
				IsFinal: true,
			}:
				log.Infof("Microsoft ASR 发送最终结果: %s", event.Result.Text)
			default:
				log.Warnf("Microsoft ASR 结果通道已满或已关闭，无法发送最终结果: %s", event.Result.Text)
			}
		} else if event.Result.Reason == common.NoMatch {
			log.Debugf("Microsoft ASR 未匹配到语音内容")
		} else {
			log.Debugf("Microsoft ASR Recognized 事件，但未发送结果，Reason: %v", event.Result.Reason)
		}
	})

	// Canceled 事件：错误处理
	recognizer.Canceled(func(event speech.SpeechRecognitionCanceledEventArgs) {
		defer event.Close()
		log.Errorf("Microsoft ASR 已取消, 原因: %v", event.Reason)
		if event.Reason == common.Error {
			log.Errorf("Microsoft ASR 错误, 错误码: %v, 详情: %s", event.ErrorCode, event.ErrorDetails)
			// 发送错误结果
			select {
			case <-ctx.Done():
				return
			case resultChan <- types.StreamingResult{
				Text:    "",
				IsFinal: true,
				Error:   fmt.Errorf("识别错误: %v, 错误码: %v, 详情: %s", event.Reason, event.ErrorCode, event.ErrorDetails),
			}:
			}
			// 触发会话停止，让清理 goroutine 能够及时响应
			select {
			case <-sessionStopped:
				// 已经关闭，不需要再次关闭
			default:
				close(sessionStopped)
			}
		}
	})

	// 开始连续识别
	log.Debugf("Microsoft ASR 开始连续识别")
	recognizer.StartContinuousRecognitionAsync()

	// 用于标记音频流是否已结束
	audioStreamEnded := make(chan struct{})

	// 启动 goroutine 推送音频数据
	go func() {
		defer close(audioStreamEnded)

		// 从 audioStream 读取 PCM 数据并推送到 Microsoft SDK
		for {
			select {
			case <-ctx.Done():
				log.Debugf("Microsoft ASR 上下文已取消")
				return
			case pcmChunk, ok := <-audioStream:
				if !ok {
					log.Debugf("Microsoft ASR 音频流已结束")
					return
				}

				// 将 float32 PCM 数据转换为 16-bit PCM bytes
				pcmBytes := float32ToPCM16Bytes(pcmChunk)

				// 推送到音频流
				err := stream.Write(pcmBytes)
				if err != nil {
					log.Errorf("Microsoft ASR 写入音频流失败: %v", err)
					// 发送错误结果
					select {
					case <-ctx.Done():
						return
					case resultChan <- types.StreamingResult{
						Text:    "",
						IsFinal: true,
						Error:   fmt.Errorf("写入音频流失败: %v", err),
					}:
					}
					// 触发会话停止，让清理 goroutine 能够及时响应
					select {
					case <-sessionStopped:
						// 已经关闭，不需要再次关闭
					default:
						close(sessionStopped)
					}
					return
				}
			}
		}
	}()

	// 启动 goroutine 处理识别结束和资源清理
	go func() {
		// 等待音频流结束
		<-audioStreamEnded

		log.Debugf("Microsoft ASR 音频流已结束，等待识别完成...")

		// 关闭音频流，触发识别结束
		// 注意：关闭流后，识别器会继续处理剩余的音频数据并触发最终结果
		stream.CloseStream()

		// 等待识别器处理剩余数据并触发最终结果
		// 给足够的时间让识别器处理并触发 Recognized 事件
		log.Debugf("Microsoft ASR 等待识别器处理剩余数据...")
		time.Sleep(2 * time.Second)

		// 停止识别
		recognizer.StopContinuousRecognitionAsync()

		// 等待会话完全停止（等待 SessionStopped 事件）
		// 增加超时时间，确保有足够时间处理所有结果
		select {
		case <-sessionStopped:
			log.Debugf("Microsoft ASR 会话已停止")
		case <-time.After(10 * time.Second):
			log.Warnf("Microsoft ASR 等待会话停止超时")
		}

		// 再等待一段时间，确保所有事件回调都已处理完成
		// 特别是 Recognized 事件可能在这之后才触发
		time.Sleep(1 * time.Second)

		// 关闭识别器
		recognizer.Close()

		// 安全关闭结果通道（使用 sync.Once 确保只关闭一次）
		closeResultChan()
		log.Debugf("Microsoft ASR 流式识别结束, 耗时: %d ms", time.Now().UnixMilli()-startTs)
	}()

	return resultChan, nil
}
