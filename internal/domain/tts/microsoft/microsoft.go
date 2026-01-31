package microsoft

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	sdkAudio "github.com/Microsoft/cognitive-services-speech-sdk-go/audio"
	"github.com/Microsoft/cognitive-services-speech-sdk-go/common"
	"github.com/Microsoft/cognitive-services-speech-sdk-go/speech"

	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

// 全局连接池：复用 SpeechConfig 对象
var (
	configPool     = make(map[string]*speech.SpeechConfig)
	configPoolLock sync.RWMutex
)

// MicrosoftTTSProvider Microsoft TTS 提供者
// 支持一次性和流式TTS，输出Opus帧
// 配置参数：subscription_key, region, voice, language, timeout
type MicrosoftTTSProvider struct {
	SubscriptionKey string
	Region          string
	Voice           string
	Language        string
	Timeout         int
	FrameDuration   int
}

// NewMicrosoftTTSProvider 创建新的 Microsoft TTS Provider
func NewMicrosoftTTSProvider(config map[string]interface{}) *MicrosoftTTSProvider {
	subscriptionKey, _ := config["subscription_key"].(string)
	region, _ := config["region"].(string)
	voice, _ := config["voice"].(string)
	language, _ := config["language"].(string)
	timeout, _ := config["timeout"].(int)
	frameDuration, _ := config["frame_duration"].(float64)

	// 设置默认值
	if voice == "" {
		voice = "zh-CN-XiaoxiaoNeural"
	}
	if language == "" {
		language = "zh-CN"
	}
	if timeout == 0 {
		timeout = 60
	}
	if frameDuration == 0 {
		frameDuration = float64(audio.FrameDuration)
	}

	return &MicrosoftTTSProvider{
		SubscriptionKey: subscriptionKey,
		Region:          region,
		Voice:           voice,
		Language:        language,
		Timeout:         timeout,
		FrameDuration:   int(frameDuration),
	}
}

// getOrCreateConfig 获取或创建 SpeechConfig（复用连接池）
func getOrCreateConfig(subscriptionKey, region, voice, language string) (*speech.SpeechConfig, error) {
	key := fmt.Sprintf("%s_%s_%s_%s", subscriptionKey, region, voice, language)

	configPoolLock.RLock()
	config, exists := configPool[key]
	configPoolLock.RUnlock()

	if exists && config != nil {
		log.Debugf("复用 Microsoft TTS SpeechConfig, key: %s", key)
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

	config.SetSpeechSynthesisVoiceName(voice)
	config.SetSpeechSynthesisLanguage(language)
	// 固定使用 MP3 格式
	config.SetSpeechSynthesisOutputFormat(common.Audio16Khz128KBitRateMonoMp3)

	configPool[key] = config
	log.Debugf("创建新的 Microsoft TTS SpeechConfig, key: %s", key)
	return config, nil
}

// TextToSpeech 一次性合成，返回Opus帧
func (p *MicrosoftTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	startTs := time.Now().UnixMilli()

	// 从连接池获取或创建 SpeechConfig
	config, err := getOrCreateConfig(p.SubscriptionKey, p.Region, p.Voice, p.Language)
	if err != nil {
		return nil, fmt.Errorf("获取 SpeechConfig 失败: %v", err)
	}

	// 创建临时文件用于音频输出
	tmpFile := fmt.Sprintf("/tmp/microsoft-tts-%d.mp3", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	// 创建音频输出配置（输出到文件）
	audioConfig, err := sdkAudio.NewAudioConfigFromWavFileOutput(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("创建音频配置失败: %v", err)
	}
	defer audioConfig.Close()

	// 创建语音合成器
	synthesizer, err := speech.NewSpeechSynthesizerFromConfig(config, audioConfig)
	if err != nil {
		return nil, fmt.Errorf("创建合成器失败: %v", err)
	}
	defer synthesizer.Close()

	// 创建独立的 context，基于 Background，避免被传入的 ctx 取消影响
	// 但设置超时时间，防止无限等待
	ttsCtx, ttsCancel := context.WithTimeout(context.Background(), time.Duration(p.Timeout)*time.Second)
	defer ttsCancel()

	// 开始合成
	task := synthesizer.SpeakTextAsync(text)

	var outcome speech.SpeechSynthesisOutcome
	// 使用独立的 ttsCtx 等待 task 完成，避免被外部 context 取消影响
	select {
	case outcome = <-task:
		// task 完成，继续处理
	case <-ttsCtx.Done():
		return nil, fmt.Errorf("合成超时: %v", ttsCtx.Err())
	}
	defer outcome.Close()

	if outcome.Error != nil {
		return nil, fmt.Errorf("合成错误: %v", outcome.Error)
	}

	// 检查结果
	if outcome.Result.Reason != common.SynthesizingAudioCompleted {
		return nil, fmt.Errorf("合成未完成, 原因: %v", outcome.Result.Reason)
	}

	// 读取临时文件并转换为 Opus 帧
	f, err := os.Open(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("打开MP3文件失败: %v", err)
	}
	defer f.Close()

	pipeReader, pipeWriter := io.Pipe()
	outputChan := make(chan []byte, 1000)

	// 写入MP3数据到pipe
	go func() {
		defer pipeWriter.Close()
		_, err := io.Copy(pipeWriter, f)
		if err != nil {
			log.Errorf("复制MP3数据到pipe失败: %v", err)
		}
	}()

	// 创建MP3解码器，使用独立的 ttsCtx 避免被外部 context 取消
	mp3Decoder, err := util.CreateAudioDecoder(ttsCtx, pipeReader, outputChan, frameDuration, "mp3")
	if err != nil {
		return nil, fmt.Errorf("创建MP3解码器失败: %v", err)
	}

	// 收集Opus帧
	var opusFrames [][]byte
	done := make(chan struct{})

	go func() {
		defer close(done)
		for frame := range outputChan {
			opusFrames = append(opusFrames, frame)
		}
	}()

	// 运行解码器
	if err := mp3Decoder.Run(startTs); err != nil {
		return nil, fmt.Errorf("MP3解码失败: %v", err)
	}

	// 等待所有帧收集完成
	<-done

	log.Infof("Microsoft TTS完成，从输入到获取音频数据结束耗时: %d ms", time.Now().UnixMilli()-startTs)
	return opusFrames, nil
}

// TextToSpeechStream 流式合成，返回Opus帧chan
func (p *MicrosoftTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	startTs := time.Now().UnixMilli()

	// 从连接池获取或创建 SpeechConfig
	config, err := getOrCreateConfig(p.SubscriptionKey, p.Region, p.Voice, p.Language)
	if err != nil {
		return nil, fmt.Errorf("获取 SpeechConfig 失败: %v", err)
	}

	// 创建语音合成器（不指定音频输出，用于流式）
	synthesizer, err := speech.NewSpeechSynthesizerFromConfig(config, nil)
	if err != nil {
		return nil, fmt.Errorf("创建合成器失败: %v", err)
	}
	// 注意：不在 defer 中立即关闭 synthesizer，让 goroutine 自己管理生命周期
	// 在 goroutine 完成时再关闭，避免过早关闭导致操作失败

	// 创建独立的 context，基于 Background，避免被传入的 ctx 取消影响
	// 但设置超时时间，防止无限等待
	ttsCtx, ttsCancel := context.WithTimeout(context.Background(), time.Duration(p.Timeout)*time.Second)
	// 注意：不在 defer 中立即取消，让 goroutine 自己管理 context 生命周期
	// 在 goroutine 完成时再取消，避免过早取消导致操作失败

	// 创建输出通道
	outputChan := make(chan []byte, 100)

	// 创建pipe用于MP3到Opus转换
	pipeReader, pipeWriter := io.Pipe()

	// 用于同步合成完成
	synthesisDone := make(chan error, 1)

	// 设置事件回调，使用 Synthesizing 事件实现真正的流式合成
	synthesizer.SynthesisStarted(func(event speech.SpeechSynthesisEventArgs) {
		defer event.Close()
		log.Debugf("Microsoft TTS 合成开始")
	})

	// Synthesizing 事件：在合成过程中实时接收音频数据块
	// 这是真正的流式合成，边合成边返回音频数据
	synthesizer.Synthesizing(func(event speech.SpeechSynthesisEventArgs) {
		defer event.Close()
		if len(event.Result.AudioData) > 0 {
			// 实时写入音频数据到 pipe
			// 注意：事件回调是顺序执行的，直接写入可以保证数据顺序
			select {
			case <-ttsCtx.Done():
				return
			default:
				if _, err := pipeWriter.Write(event.Result.AudioData); err != nil {
					log.Errorf("写入pipe失败: %v", err)
					return
				}
			}
		}
	})

	synthesizer.SynthesisCompleted(func(event speech.SpeechSynthesisEventArgs) {
		defer event.Close()
		log.Debugf("Microsoft TTS 合成完成，总计 %d 字节", len(event.Result.AudioData))
		// 关闭 pipeWriter，表示音频数据已全部写入
		pipeWriter.Close()
		synthesisDone <- nil
	})

	synthesizer.SynthesisCanceled(func(event speech.SpeechSynthesisEventArgs) {
		defer event.Close()
		var err error
		// event.Result 是结构体，需要传递指针
		cancellation, cancelErr := speech.NewCancellationDetailsFromSpeechSynthesisResult(&event.Result)
		if cancelErr == nil {
			if cancellation.Reason == common.Error {
				err = fmt.Errorf("合成错误: %v, 错误码: %v, 详情: %s", cancellation.Reason, cancellation.ErrorCode, cancellation.ErrorDetails)
			} else {
				err = fmt.Errorf("合成已取消: %v", cancellation.Reason)
			}
		} else {
			err = fmt.Errorf("合成已取消")
		}
		pipeWriter.Close()
		select {
		case synthesisDone <- err:
		default:
		}
	})

	// 启动goroutine处理流式合成
	go func() {
		defer func() {
			// 注意：不需要关闭 outputChan，因为 mp3Decoder.Run() 内部会关闭它
			synthesizer.Close() // 在 goroutine 完成时关闭 synthesizer
			ttsCancel()         // 在 goroutine 完成时取消 context
		}()

		// 创建MP3解码器，使用独立的 ttsCtx 避免被外部 context 取消
		mp3Decoder, err := util.CreateAudioDecoder(ttsCtx, pipeReader, outputChan, frameDuration, "mp3")
		if err != nil {
			log.Errorf("创建MP3解码器失败: %v", err)
			pipeWriter.Close()
			return
		}

		// 启动解码过程（在后台运行）
		decoderDone := make(chan error, 1)
		go func() {
			decoderDone <- mp3Decoder.Run(startTs)
		}()

		// 开始合成（使用 SpeakTextAsync，它会触发 Synthesizing 事件）
		task := synthesizer.SpeakTextAsync(text)

		var outcome speech.SpeechSynthesisOutcome
		// 等待合成任务完成
		select {
		case outcome = <-task:
			// task 完成
		case <-ttsCtx.Done():
			log.Errorf("Microsoft TTS流式合成超时: %v", ttsCtx.Err())
			pipeWriter.Close()
			return
		}
		defer outcome.Close()

		if outcome.Error != nil {
			log.Errorf("Microsoft TTS流式合成错误: %v", outcome.Error)
			pipeWriter.Close()
			return
		}

		// 等待合成完成（通过事件回调）
		select {
		case err := <-synthesisDone:
			if err != nil {
				log.Errorf("Microsoft TTS合成过程错误: %v", err)
			}
		case <-ttsCtx.Done():
			log.Errorf("Microsoft TTS流式合成超时: %v", ttsCtx.Err())
			pipeWriter.Close()
			return
		}

		// 等待解码器完成
		select {
		case err := <-decoderDone:
			if err != nil {
				log.Errorf("MP3解码失败: %v", err)
			} else {
				log.Infof("Microsoft TTS流式合成完成，耗时: %d ms", time.Now().UnixMilli()-startTs)
			}
		case <-ttsCtx.Done():
			log.Debugf("Microsoft TTS流式合成取消, 文本: %s", text)
		}
	}()

	return outputChan, nil
}
