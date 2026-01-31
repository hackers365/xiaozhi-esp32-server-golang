package chat

import (
	"context"
	"fmt"
	"sync"
	"time"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/pool"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

type TTSQueueItem struct {
	ctx         context.Context
	llmResponse llm_common.LLMResponseStruct
	onStartFunc func()
	onEndFunc   func(err error)
}

// TTSManager 负责TTS相关的处理
// 可以根据需要扩展字段
// 目前无状态，但可后续扩展

type TTSManagerOption func(*TTSManager)

type TTSManager struct {
	clientState     *ClientState
	serverTransport *ServerTransport
	ttsQueue        *util.Queue[TTSQueueItem]

	// 聊天历史音频缓存：持续累积多段TTS音频（Opus帧数组）
	audioHistoryBuffer [][]byte
	audioMutex         sync.Mutex
}

// NewTTSManager 只接受WithClientState
func NewTTSManager(clientState *ClientState, serverTransport *ServerTransport, opts ...TTSManagerOption) *TTSManager {
	t := &TTSManager{
		clientState:     clientState,
		serverTransport: serverTransport,
		ttsQueue:        util.NewQueue[TTSQueueItem](10),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// 启动TTS队列消费协程
func (t *TTSManager) Start(ctx context.Context) {
	t.processTTSQueue(ctx)
}

func (t *TTSManager) processTTSQueue(ctx context.Context) {
	for {
		item, err := t.ttsQueue.Pop(ctx, 0) // 阻塞式
		if err != nil {
			if err == util.ErrQueueCtxDone {
				return
			}
			continue
		}

		log.Debugf("processTTSQueue start, text: %s", item.llmResponse.Text)

		if item.onStartFunc != nil {
			item.onStartFunc()
		}
		err = t.handleTts(item.ctx, item.llmResponse)
		if item.onEndFunc != nil {
			item.onEndFunc(err)
		}
		log.Debugf("processTTSQueue end, text: %s", item.llmResponse.Text)

	}
}

func (t *TTSManager) ClearTTSQueue() {
	t.ttsQueue.Clear()
}

// 处理文本内容响应（异步 TTS 入队）
func (t *TTSManager) handleTextResponse(ctx context.Context, llmResponse llm_common.LLMResponseStruct, isSync bool) error {
	if llmResponse.Text == "" {
		return nil
	}

	ttsQueueItem := TTSQueueItem{ctx: ctx, llmResponse: llmResponse}
	endChan := make(chan bool, 1)
	ttsQueueItem.onEndFunc = func(err error) {
		select {
		case endChan <- true:
		default:
		}
	}

	t.ttsQueue.Push(ttsQueueItem)

	if isSync {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-endChan:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("TTS 处理上下文已取消")
		case <-timer.C:
			return fmt.Errorf("TTS 处理超时")
		}
	}

	return nil
}

// getTTSProviderInstance 获取TTS Provider实例（使用provider+音色作为资源池唯一key）
func (t *TTSManager) getTTSProviderInstance() (*pool.ResourceWrapper[tts.TTSProvider], error) {
	// 获取TTS配置和provider
	var ttsConfig map[string]interface{}
	var ttsProvider string

	if t.clientState.SpeakerTTSConfig != nil && len(t.clientState.SpeakerTTSConfig) > 0 {
		// 使用声纹TTS配置
		if provider, ok := t.clientState.SpeakerTTSConfig["provider"].(string); ok {
			ttsProvider = provider
		} else {
			log.Warnf("声纹TTS配置中缺少 provider，使用默认配置")
			ttsProvider = t.clientState.DeviceConfig.Tts.Provider
			ttsConfig = t.clientState.DeviceConfig.Tts.Config
		}
		// 深拷贝配置
		ttsConfig = make(map[string]interface{})
		for k, v := range t.clientState.SpeakerTTSConfig {
			ttsConfig[k] = v
		}
	} else {
		// 使用默认TTS配置
		ttsProvider = t.clientState.DeviceConfig.Tts.Provider
		ttsConfig = t.clientState.DeviceConfig.Tts.Config
	}

	// 提取音色ID用于组合资源池key
	voiceID := extractVoiceID(ttsConfig)

	log.Debugf("getTTSProviderInstance, ttsProvider: %s, voiceID: %s", ttsProvider, voiceID)
	// 组合资源池key：provider:voiceID（如果有音色ID）
	poolKey := ttsProvider
	if voiceID != "" {
		poolKey = fmt.Sprintf("%s:%s", ttsProvider, voiceID)
	}

	// 从资源池获取TTS资源（使用provider+音色作为唯一key）
	ttsWrapper, err := pool.Acquire[tts.TTSProvider]("tts", poolKey, ttsConfig)
	if err != nil {
		log.Errorf("获取TTS资源失败: %v", err)
		return nil, fmt.Errorf("获取TTS资源失败: %v", err)
	}

	return ttsWrapper, nil
}

// extractVoiceID 从配置中提取音色ID
func extractVoiceID(config map[string]interface{}) string {
	if config == nil {
		return ""
	}

	// 尝试从config中获取provider类型
	provider, _ := config["provider"].(string)

	// cosyvoice使用spk_id字段
	if provider == "cosyvoice" {
		if spkID, ok := config["spk_id"].(string); ok && spkID != "" {
			return spkID
		}
		return ""
	}

	// minimax和其他provider：使用voice
	if voice, ok := config["voice"].(string); ok && voice != "" {
		return voice
	}

	return ""
}

// 同步 TTS 处理
func (t *TTSManager) handleTts(ctx context.Context, llmResponse llm_common.LLMResponseStruct) error {
	log.Debugf("handleTts start, text: %s", llmResponse.Text)
	defer log.Debugf("handleTts end, text: %s", llmResponse.Text)
	if llmResponse.Text == "" {
		return nil
	}

	// 获取TTS Provider实例
	ttsWrapper, err := t.getTTSProviderInstance()
	if err != nil {
		log.Errorf("获取TTS Provider实例失败: %v", err)
		return err
	}
	defer pool.Release(ttsWrapper)

	ttsProviderInstance := ttsWrapper.GetProvider()

	// 使用带上下文的TTS处理
	outputChan, err := ttsProviderInstance.TextToSpeechStream(ctx, llmResponse.Text, t.clientState.OutputAudioFormat.SampleRate, t.clientState.OutputAudioFormat.Channels, t.clientState.OutputAudioFormat.FrameDuration)
	if err != nil {
		log.Errorf("生成 TTS 音频失败: %v", err)
		return fmt.Errorf("生成 TTS 音频失败: %v", err)
	}

	if err := t.serverTransport.SendSentenceStart(llmResponse.Text); err != nil {
		log.Errorf("发送 TTS 文本失败: %s, %v", llmResponse.Text, err)
		return fmt.Errorf("发送 TTS 文本失败: %s, %v", llmResponse.Text, err)
	}

	// 发送音频帧
	if err := t.SendTTSAudio(ctx, outputChan, llmResponse.IsStart); err != nil {
		log.Errorf("发送 TTS 音频失败: %s, %v", llmResponse.Text, err)
		return fmt.Errorf("发送 TTS 音频失败: %s, %v", llmResponse.Text, err)
	}

	if err := t.serverTransport.SendSentenceEnd(llmResponse.Text); err != nil {
		log.Errorf("发送 TTS 文本失败: %s, %v", llmResponse.Text, err)
		return fmt.Errorf("发送 TTS 文本失败: %s, %v", llmResponse.Text, err)
	}

	return nil
}

// getAlignedDuration 计算当前时间与开始时间的差值，向上对齐到frameDuration
func getAlignedDuration(startTime time.Time, frameDuration time.Duration) time.Duration {
	elapsed := time.Since(startTime)
	// 向上对齐到frameDuration
	alignedMs := ((elapsed.Milliseconds() + frameDuration.Milliseconds() - 1) / frameDuration.Milliseconds()) * frameDuration.Milliseconds()
	return time.Duration(alignedMs) * time.Millisecond
}

func (t *TTSManager) SendTTSAudio(ctx context.Context, audioChan chan []byte, isStart bool) error {
	totalFrames := 0 // 跟踪已发送的总帧数

	isStatistic := true
	//首次发送180ms音频, 根据outputAudioFormat.FrameDuration计算
	cacheFrameCount := 120 / t.clientState.OutputAudioFormat.FrameDuration
	/*if cacheFrameCount > 20 || cacheFrameCount < 3 {
		cacheFrameCount = 5
	}*/

	// 记录开始发送的时间戳
	startTime := time.Now()

	// 基于绝对时间的精确流控
	frameDuration := time.Duration(t.clientState.OutputAudioFormat.FrameDuration) * time.Millisecond

	log.Debugf("SendTTSAudio 开始，缓存帧数: %d, 帧时长: %v", cacheFrameCount, frameDuration)

	// 使用滑动窗口机制，确保对端始终缓存 cacheFrameCount 帧数据
	for {
		// 计算下一帧应该发送的时间点
		nextFrameTime := startTime.Add(time.Duration(totalFrames-cacheFrameCount) * frameDuration)
		now := time.Now()

		// 如果下一帧时间还没到，需要等待
		if now.Before(nextFrameTime) {
			sleepDuration := nextFrameTime.Sub(now)
			//log.Debugf("SendTTSAudio 流控等待: %v", sleepDuration)
			time.Sleep(sleepDuration)
		}

		// 尝试获取并发送下一帧
		select {
		case <-ctx.Done():
			log.Debugf("SendTTSAudio context done, exit")
			return nil
		case frame, ok := <-audioChan:
			if !ok {
				// 通道已关闭，所有帧已处理完毕
				// 为确保终端播放完成：等待已发送帧的总时长与从开始发送以来的实际耗时之间的差值
				elapsed := time.Since(startTime)
				totalDuration := time.Duration(totalFrames) * frameDuration
				if totalDuration > elapsed {
					waitDuration := totalDuration - elapsed
					log.Debugf("SendTTSAudio 等待客户端播放剩余缓冲: %v (totalFrames=%d, frameDuration=%v)", waitDuration, totalFrames, frameDuration)
					time.Sleep(waitDuration)
				}

				log.Debugf("SendTTSAudio audioChan closed, exit, 总共发送 %d 帧", totalFrames)
				return nil
			}
			// 发送当前帧
			if err := t.serverTransport.SendAudio(frame); err != nil {
				log.Errorf("发送 TTS 音频失败: 第 %d 帧, len: %d, 错误: %v", totalFrames, len(frame), err)
				return fmt.Errorf("发送 TTS 音频 len: %d 失败: %v", len(frame), err)
			}

			// 累积音频数据到历史缓存（每一帧作为独立的[]byte）
			t.audioMutex.Lock()
			// 复制帧数据，避免引用问题
			frameCopy := make([]byte, len(frame))
			copy(frameCopy, frame)
			t.audioHistoryBuffer = append(t.audioHistoryBuffer, frameCopy)
			t.audioMutex.Unlock()

			totalFrames++
			if totalFrames%100 == 0 {
				log.Debugf("SendTTSAudio 已发送 %d 帧", totalFrames)
			}

			// 统计信息记录（仅在开始时记录一次）
			if isStart && isStatistic && totalFrames == 1 {
				log.Debugf("从接收音频结束 asr->llm->tts首帧 整体 耗时: %d ms", t.clientState.GetAsrLlmTtsDuration())
				isStatistic = false
			}
		}
	}
}

// ClearAudioHistory 清空TTS音频历史缓存
func (t *TTSManager) ClearAudioHistory() {
	t.audioMutex.Lock()
	defer t.audioMutex.Unlock()
	t.audioHistoryBuffer = nil
}

// GetAndClearAudioHistory 获取并清空TTS音频历史缓存
func (t *TTSManager) GetAndClearAudioHistory() [][]byte {
	t.audioMutex.Lock()
	defer t.audioMutex.Unlock()
	data := t.audioHistoryBuffer
	t.audioHistoryBuffer = nil
	return data
}
