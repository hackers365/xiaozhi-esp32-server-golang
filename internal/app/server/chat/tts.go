package chat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	tts_types "xiaozhi-esp32-server-golang/internal/domain/tts/types"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
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
		if item.onStartFunc != nil {
			item.onStartFunc()
		}
		err = t.handleTts(item.ctx, item.llmResponse)
		if item.onEndFunc != nil {
			item.onEndFunc(err)
		}
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

// 同步 TTS 处理
func (t *TTSManager) handleTts(ctx context.Context, llmResponse llm_common.LLMResponseStruct) error {
	log.Debugf("handleTts start, text: %s", llmResponse.Text)
	if llmResponse.Text == "" {
		return nil
	}

	// 使用带上下文的TTS处理
	outputChan, err := t.clientState.TTSProvider.TextToSpeechStream(ctx, llmResponse.Text, t.clientState.OutputAudioFormat.SampleRate, t.clientState.OutputAudioFormat.Channels, t.clientState.OutputAudioFormat.FrameDuration)
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

func (t *TTSManager) EinoTtsComponents(ctx context.Context, input *schema.StreamReader[*schema.StreamReader[tts_types.TtsChunk]]) (*schema.Message, error) {
	var finalMsg schema.Message

	var stringBuffer bytes.Buffer
	isFirst := true
	// 处理一个句子的音频流
	processSentenceStream := func(sentenceStream *schema.StreamReader[tts_types.TtsChunk]) error {
		var sentenceText string
		audioChan := make(chan []byte, 100)

		// 读取句子流的第一个 chunk（应该包含文本标记）
		firstChunk, err := sentenceStream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil // 空句子流，跳过
			}
			return fmt.Errorf("读取句子流失败: %v", err)
		}

		// 第一个 chunk 应该是句子开始标记，包含文本
		if firstChunk.Text != "" {
			sentenceText = firstChunk.Text
		}

		// 在 goroutine 中读取该句子的音频帧并发送到 channel
		go func() {
			defer close(audioChan)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				ttsChunk, err := sentenceStream.Recv()
				if err != nil {
					if err == io.EOF {
						return
					}
					log.Errorf("读取句子音频流失败: %v", err)
					return
				}

				// 如果有音频数据，发送到通道
				if len(ttsChunk.Data) > 0 {
					select {
					case <-ctx.Done():
						return
					case audioChan <- ttsChunk.Data:
					}
				}
			}
		}()

		// 发送句子开始信号
		if sentenceText != "" {
			if err := t.serverTransport.SendSentenceStart(sentenceText); err != nil {
				log.Errorf("发送 TTS 文本失败: %s, %v", sentenceText, err)
				return err
			}
		}

		// 发送该句子的所有音频帧（从 channel 中流式读取并发送）
		if err := t.SendTTSAudio(ctx, audioChan, isFirst); err != nil {
			log.Errorf("发送 TTS 音频失败: %v", err)
			return err
		}

		isFirst = false

		// 发送句子结束信号
		if sentenceText != "" {
			if err := t.serverTransport.SendSentenceEnd(sentenceText); err != nil {
				log.Errorf("发送 TTS 文本失败: %s, %v", sentenceText, err)
				return err
			}
		}

		// 流式输出当前句子的消息
		if sentenceText != "" {
			stringBuffer.WriteString(sentenceText)
		}

		return nil
	}

	// 接收每个句子的音频流
	for {
		select {
		case <-ctx.Done():
			log.Debugf("EinoTtsComponents context done, exit")
			return &finalMsg, nil
		default:
		}

		sentenceStream, err := input.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Errorf("接收句子流失败: %v", err)
			return &finalMsg, err
		}

		// 处理当前句子的音频流
		if err := processSentenceStream(sentenceStream); err != nil {
			log.Errorf("处理句子音频流失败: %v", err)
			return &finalMsg, err
		}
	}

	finalMsg.Content = stringBuffer.String()

	return &finalMsg, nil
}
