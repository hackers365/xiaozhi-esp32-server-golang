package chat

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/domain/asr"
	asr_types "xiaozhi-esp32-server-golang/internal/domain/asr/types"
	"xiaozhi-esp32-server-golang/internal/domain/audio"
	chathooks "xiaozhi-esp32-server-golang/internal/domain/chat/hooks"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
	"xiaozhi-esp32-server-golang/internal/domain/vad/inter"
	"xiaozhi-esp32-server-golang/internal/pool"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

type ASRManagerOption func(*ASRManager)

const maxFirstSpeechPreAudioMs = 200

// AsrMessageSaveCallback 消息保存回调函数类型
type AsrMessageSaveCallback func(userMsg *schema.Message, messageID string, audioData []float32)

type ASRManager struct {
	clientState     *ClientState
	serverTransport *ServerTransport
	session         *ChatSession // 用于访问 speakerManager

	// ASR 资源作为私有字段管理
	asrResource *pool.ResourceWrapper[asr.AsrProvider]
	resourceMu  sync.RWMutex // 保护资源访问
}

func NewASRManager(clientState *ClientState, serverTransport *ServerTransport, opts ...ASRManagerOption) *ASRManager {
	asr := &ASRManager{
		clientState:     clientState,
		serverTransport: serverTransport,
		session:         nil, // 稍后通过 SetSession 设置
	}
	for _, opt := range opts {
		opt(asr)
	}
	return asr
}

func (a *ASRManager) runAudioIdleTimeoutWatchdog(ctx context.Context) {
	state := a.clientState
	if state == nil {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !state.UsesAudioIdleClock() || !state.AudioIdleStarted() || state.AudioIdlePaused() {
				continue
			}
			if !state.ShouldCountAudioIdleTimeout() || state.Asr.HasReceivedText() {
				continue
			}
			if state.GetClientVoiceStop() || state.AudioIdleTimeoutPending() {
				continue
			}

			elapsed := state.GetAudioIdleElapsed(time.Now())
			threshold := time.Duration(state.GetMaxIdleDuration()) * time.Millisecond
			if elapsed < threshold {
				continue
			}
			if !state.MarkAudioIdleTimeoutPending() {
				continue
			}

			if !state.Asr.HasOpenAudioInput() {
				log.Infof(
					"音频空闲超时，当前无活动ASR流，直接关闭会话: device=%s, mode=%s, elapsed=%dms, threshold=%dms",
					state.DeviceID,
					state.ListenMode,
					elapsed.Milliseconds(),
					state.GetMaxIdleDuration(),
				)
				if a.session != nil {
					a.session.CloseWithReason(chatSessionCloseReasonAudioIdleTimeout)
				} else {
					state.ClearAudioIdleTimeoutPending()
				}
				continue
			}

			log.Infof(
				"音频空闲超时，触发ASR收口: device=%s, mode=%s, elapsed=%dms, threshold=%dms",
				state.DeviceID,
				state.ListenMode,
				elapsed.Milliseconds(),
				state.GetMaxIdleDuration(),
			)
			state.OnVoiceSilence()
		}
	}
}

// ProcessVadAudio 启动VAD音频处理
func (a *ASRManager) ProcessVadAudio(ctx context.Context) {
	state := a.clientState
	go func() {
		hasTriggeredCancel := true // 标志位，记录是否已触发过取消操作（当 voiceDuration > 120 时）
		hasLoggedFirstTextExtendedWait := false
		speakerInterruptTriggered := atomic.Bool{}
		speakerPeekInFlight := atomic.Bool{}
		lastSpeakerPeekDoneAt := atomic.Int64{}
		var speakerPeekAudioMs int64
		var speakerPeekRequestSeq uint64
		const speakerPeekInterval = 200 * time.Millisecond
		const firstSpeakerPeekAudioThresholdMs int64 = 400
		audioFormat := state.InputAudioFormat
		// 使用一个足够大的缓冲区用于解码（假设最大帧时长为120ms）
		maxFrameSize := audioFormat.SampleRate * audioFormat.Channels * 120 / 1000
		audioProcesser, err := audio.GetAudioProcesser(audioFormat.SampleRate, audioFormat.Channels, 20) // 传入一个默认值用于创建解码器
		if err != nil {
			log.Errorf("获取解码器失败: %v", err)
			return
		}

		// 从第一帧实际数据中获取帧大小和帧时长
		var frameSize int
		var frameDurationMs int
		var vadNeedGetCount int // VAD需要的帧数，会在第一帧后计算

		// VAD 资源改为懒加载 + 空闲释放，避免长期独占资源池实例。
		var vadWrapper *pool.ResourceWrapper[inter.VAD]
		var vadProvider inter.VAD
		var vadLastUseAt time.Time
		const vadIdleReleaseTimeout = 2 * time.Second
		vadIdleTicker := time.NewTicker(time.Second)
		defer vadIdleTicker.Stop()
		needVad := !(state.Asr.AutoEnd || state.ListenMode == "manual")
		vadProviderName := state.DeviceConfig.Vad.Provider
		vadProviderConfig := state.DeviceConfig.Vad.Config
		effectiveVadProviderName := vadProviderName
		if configProvider, ok := vadProviderConfig["provider"].(string); ok && configProvider != "" {
			effectiveVadProviderName = configProvider
		}
		isSileroVAD := effectiveVadProviderName == "silero_vad"
		keepVadStateBetweenFrames := isSileroVAD || effectiveVadProviderName == "ten_vad"
		releaseVad := func(reason string) {
			if vadWrapper == nil {
				return
			}
			pool.Release(vadWrapper)
			vadWrapper = nil
			vadProvider = nil
			vadLastUseAt = time.Time{}
			log.Debugf("释放VAD资源: device=%s, reason=%s", state.DeviceID, reason)
		}
		defer releaseVad("process_exit")
		ensureVad := func() bool {
			if !needVad {
				return false
			}
			if vadProvider != nil {
				return true
			}

			// 检查 provider 是否为空，如果为空则记录警告
			if vadProviderName == "" {
				log.Warnf("VAD provider 为空，尝试从 config 中获取")
			} else {
				log.Debugf("获取VAD资源: provider=%s", vadProviderName)
			}

			wrapper, err := pool.Acquire[inter.VAD](
				"vad",
				vadProviderName,
				vadProviderConfig,
			)
			if err != nil {
				log.Errorf("获取VAD资源失败: provider=%s, config=%+v, error=%v", vadProviderName, vadProviderConfig, err)
				return false
			}
			vadWrapper = wrapper
			vadProvider = wrapper.GetProvider()
			vadLastUseAt = time.Now()
			return true
		}
		for {
			// 使用最大帧大小作为缓冲区，解码后会得到实际帧大小
			pcmFrame := make([]float32, maxFrameSize)

			select {
			case <-vadIdleTicker.C:
				if vadWrapper != nil && !vadLastUseAt.IsZero() && time.Since(vadLastUseAt) >= vadIdleReleaseTimeout {
					releaseVad("idle_timeout")
				}
				continue
			case opusFrame, ok := <-state.OpusAudioBuffer:
				//log.Debugf("processAsrAudio 收到音频数据, len: %d", len(opusFrame))
				if !ok {
					log.Debugf("processAsrAudio 音频通道已关闭")
					return
				}

				var skipVad bool
				var haveVoice bool
				clientHaveVoice := state.GetClientHaveVoice()
				if state.ListenMode == "manual" {
					skipVad = true         //跳过vad
					clientHaveVoice = true //之前有声音
					haveVoice = true       //本次有声音
				} else if state.Asr.AutoEnd {
					skipVad = true   // 仍由 provider 控制 stop，但不改变 idle 语义
					haveVoice = true // 本次音频直接进入 ASR
				}

				if state.GetClientVoiceStop() { //已停止 说话 则不接收音频数据
					//log.Infof("客户端停止说话, 跳过音频数据")
					continue
				}

				//log.Debugf("clientVoiceStop: %+v, asrDataSize: %d, listenMode: %s, isSkipVad: %v\n", state.GetClientVoiceStop(), state.AsrAudioBuffer.GetAsrDataSize(), state.ListenMode, skipVad)

				n, err := audioProcesser.DecoderFloat32(opusFrame, pcmFrame)
				if err != nil {
					log.Errorf("解码失败: %v", err)
					continue
				}

				// 从实际解码后的数据动态计算帧大小和帧时长
				if frameSize == 0 {
					// 第一帧：从实际解码的数据计算帧信息
					frameSize = n
					samplesPerChannel := n / audioFormat.Channels
					frameDurationMs = samplesPerChannel * 1000 / audioFormat.SampleRate
					audioFormat.FrameDuration = frameDurationMs

					// 计算 VAD 需要的帧数
					vadNeedGetCount = 1
					log.Debugf("从实际音频数据计算帧信息: frameSize=%d, frameDurationMs=%d, vadNeedGetCount=%d", frameSize, frameDurationMs, vadNeedGetCount)
				}

				var vadPcmData []float32
				pcmData := pcmFrame[:n]
				speakerPcmData := pcmFrame[:n]

				// 检查帧大小是否一致（正常情况下应该一致，但不一致时使用实际值）
				if n != frameSize {
					log.Debugf("帧大小不一致: 期望=%d, 实际=%d，使用实际值", frameSize, n)
					// 重新计算这一帧的时长
					samplesPerChannel := n / audioFormat.Channels
					currentFrameDurationMs := samplesPerChannel * 1000 / audioFormat.SampleRate
					frameSize = n
					frameDurationMs = currentFrameDurationMs
					audioFormat.FrameDuration = frameDurationMs
				}

				if !skipVad && needVad {
					if !ensureVad() {
						continue
					}
					//decode opus to pcm
					state.AsrAudioBuffer.AddAsrAudioData(pcmData)

					// 计算 VAD 需要的最小数据量
					vadNeedMinSize := frameSize

					if state.AsrAudioBuffer.GetAsrDataSize() >= vadNeedMinSize {
						if isSileroVAD {
							vadPcmData = pcmData
						} else {
							vadPcmData = state.AsrAudioBuffer.GetAsrData(vadNeedGetCount, frameSize)
						}

						//如果已经检测到语音, 则不进行vad检测, 直接将pcmData传给asr
						// 使用循环外获取的VAD资源进行检测
						if !keepVadStateBetweenFrames {
							vadLastUseAt = time.Now()
							if err := vadProvider.Reset(); err != nil {
								log.Errorf("重置vad失败: %v", err)
								continue
							}
						}

						// 进行VAD检测
						vadLastUseAt = time.Now()
						haveVoice, err = vadProvider.IsVADExt(vadPcmData, audioFormat.SampleRate, frameSize)
						if err != nil {
							log.Errorf("processAsrAudio VAD检测失败: %v", err)
							continue
						}

						//首次触发识别到语音时,为了语音数据完整性 将vadPcmData赋值给pcmData, 之后的音频数据全部进入asr
						if haveVoice && !clientHaveVoice {
							//首次检测到语音时，最多只保留200ms的前静音数据
							currentFrameSamples := len(pcmData)
							allData := state.AsrAudioBuffer.GetAndClearAllData()
							pcmData = trimFirstSpeechAudio(allData, currentFrameSamples, audioFormat.SampleRate, audioFormat.Channels)
						}
					}
					//log.Debugf("isVad, pcmData len: %d, vadPcmData len: %d, haveVoice: %v", len(pcmData), len(vadPcmData), haveVoice)
				}

				if haveVoice {
					hasLoggedFirstTextExtendedWait = false
					//log.Infof("检测到语音, len: %d", len(pcmData))
					state.SetClientHaveVoice(true)
					state.SetClientHaveVoiceLastTime(time.Now().UnixMilli())
					state.Vad.ResetIdleDuration()
					// 累积检测到声音的时长（同时更新一次过程中的时长）
					state.Vad.AddVoiceDuration(int64(frameDurationMs))

					continuousVoiceDuration := state.Vad.GetVoiceContinuousDuration()
					if state.IsRealTime() && viper.GetInt("chat.realtime_mode") == 1 && continuousVoiceDuration > 360 {
						// 只有在未触发过的情况下才执行，确保只执行一次
						if !hasTriggeredCancel {
							if a.session != nil && a.session.isRealtimeMcpAudioGateActive() {
								log.Debugf("设备 %s realtime媒体播放门控激活，跳过VAD打断", state.DeviceID)
								hasTriggeredCancel = true
							} else {
								//realtime模式下, 如果此时有正在进行的llm和tts则取消掉
								log.Debugf("realtime模式vad打断下 && 语音时长超过%d ms 如果此时有正在进行的llm和tts则取消掉", continuousVoiceDuration)
								if a.session != nil {
									a.session.StopAssistantOutputAfterAsrWithReason(true, "ASRManager.ProcessVadAudio realtime_mode=1 VAD interrupt")
								} else {
									state.AfterAsrSessionCtx.CancelWithReason("ASRManager.ProcessVadAudio: realtime_mode=1 VAD interrupt")
								}
								hasTriggeredCancel = true // 标记为已触发
							}
						}
					}
				} else {
					state.Vad.AddIdleDuration(int64(frameDurationMs))
					state.Vad.ResetVoiceContinuousDuration()

					// 没有声音时，如果之前也没有语音，则重置累积的声音时长
					// 如果之前有语音但本次没有，保留时长值，让后续逻辑判断是否应该重置
					if !clientHaveVoice {
						speakerInterruptTriggered.Store(false)
						lastSpeakerPeekDoneAt.Store(0)
						speakerPeekAudioMs = 0
						//保留近10帧
						/*
							if state.AsrAudioBuffer.GetFrameCount(frameSize) > vadNeedGetCount*3 {
								state.AsrAudioBuffer.RemoveAsrAudioData(1, frameSize)
							}*/
						continue
					}
				}

				if clientHaveVoice || haveVoice {
					// 首次命中语音时也要立刻转发当前缓存帧，避免极短语音整段未送入 ASR。

					//vad识别成功, 往asr音频通道里发送数据
					//log.Infof("vad识别成功, 往asr音频通道里发送数据, len: %d", len(pcmData))
					state.Asr.AddAudioData(pcmData)

					// 声纹只接收当前判定为有声的帧，避免将首段前导静音和尾静音送入识别流。
					if haveVoice &&
						state.IsSpeakerEnabled() && state.HasSpeakerGroups() &&
						a.session != nil && a.session.speakerManager != nil {
						// 首次检测到语音时，启动流式识别
						if !a.session.speakerManager.IsActive() {
							sampleRate := audioFormat.SampleRate
							agentId := a.session.clientState.AgentID
							if err := a.session.speakerManager.StartStreaming(ctx, sampleRate, agentId); err != nil {
								log.Warnf("启动声纹识别流失败: %v", err)
							} else {
								speakerInterruptTriggered.Store(false)
								lastSpeakerPeekDoneAt.Store(0)
								speakerPeekAudioMs = 0
							}
						}

						// 发送音频块
						if err := a.session.speakerManager.SendAudioChunk(ctx, speakerPcmData); err != nil {
							log.Warnf("发送音频块到声纹识别服务失败: %v", err)
						} else if a.session.speakerManager.IsActive() {
							if audioFormat.Channels > 0 && audioFormat.SampleRate > 0 {
								speakerPeekAudioMs += int64(len(speakerPcmData)/audioFormat.Channels) * 1000 / int64(audioFormat.SampleRate)
							}

							if state.IsRealTime() &&
								viper.GetInt("chat.realtime_mode") == 3 &&
								!speakerInterruptTriggered.Load() &&
								speakerPeekAudioMs >= firstSpeakerPeekAudioThresholdMs {
								now := time.Now()
								lastDoneAt := lastSpeakerPeekDoneAt.Load()
								if (lastDoneAt <= 0 || now.Sub(time.Unix(0, lastDoneAt)) >= speakerPeekInterval) &&
									speakerPeekInFlight.CompareAndSwap(false, true) {
									reqSeq := atomic.AddUint64(&speakerPeekRequestSeq, 1)
									requestID := fmt.Sprintf("peek_%d_%d", now.UnixMilli(), reqSeq)

									go func(reqID string) {
										defer func() {
											lastSpeakerPeekDoneAt.Store(time.Now().UnixNano())
											speakerPeekInFlight.Store(false)
										}()

										if a.session == nil || a.session.speakerManager == nil || !a.session.speakerManager.IsActive() {
											return
										}

										peekCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
										defer cancel()

										peekResult, throttled, err := a.session.speakerManager.PeekAndIdentify(peekCtx, reqID)
										if err != nil {
											if ctx.Err() == nil {
												log.Debugf("声纹peek失败: device=%s, request_id=%s, err=%v", state.DeviceID, reqID, err)
											}
											return
										}
										if throttled {
											return
										}
										if peekResult == nil || !peekResult.Identified {
											return
										}
										if !speakerInterruptTriggered.CompareAndSwap(false, true) {
											return
										}

										log.Infof(
											"realtime模式声纹peek命中，立即打断: device=%s, speaker=%s, confidence=%.4f, threshold=%.4f",
											state.DeviceID,
											peekResult.SpeakerName,
											peekResult.Confidence,
											peekResult.Threshold,
										)
										if a.session != nil && a.session.isRealtimeMcpAudioGateActive() {
											log.Debugf("设备 %s realtime媒体播放门控激活，跳过speaker peek打断", state.DeviceID)
											return
										}
										a.session.MarkTurnSpeakerInterrupted()
										if a.session != nil {
											a.session.StopAssistantOutputAfterAsrWithReason(true, "ASRManager.ProcessVadAudio realtime_mode=3 speaker peek interrupt")
										} else {
											state.AfterAsrSessionCtx.CancelWithReason("ASRManager.ProcessVadAudio: realtime_mode=3 speaker peek interrupt")
										}
									}(requestID)
								}
							}
						}
					}
				}

				//已经有语音了, 但本次没有检测到语音, 则需要判断是否已经停止说话
				lastHaveVoiceTime := state.GetClientHaveVoiceLastTime()

				if clientHaveVoice && lastHaveVoiceTime > 0 && !haveVoice {
					// 判断有音频的语音时长，如果小于300ms则重置clientHaveVoice，避免短时间语音造成的误判
					voiceDurationInSession := state.Vad.GetVoiceDurationInSession()
					if voiceDurationInSession < 100 {
						log.Debugf("语音时长过短 (%dms < 300ms)，重置clientHaveVoice", voiceDurationInSession)
						state.SetClientHaveVoice(false)
						state.Vad.ResetVoiceDuration()
						speakerInterruptTriggered.Store(false)
						lastSpeakerPeekDoneAt.Store(0)
						speakerPeekAudioMs = 0
						continue
					}

					idleDuration := state.Vad.GetIdleDuration()
					if state.IsRealTime() && !state.Asr.HasReceivedText() {
						preTextSilenceDuration := state.GetPreAsrTextSilenceDuration()
						if idleDuration <= preTextSilenceDuration {
							log.Debugf(
								"realtime模式尚未收到ASR首文本，延迟按静音阈值收口: status=%s, idle=%dms, pre_text_timeout=%dms, voice_duration=%dms, voice_duration_in_session=%dms, history_audio_samples=%d",
								state.Status,
								idleDuration,
								preTextSilenceDuration,
								state.Vad.GetVoiceDuration(),
								voiceDurationInSession,
								state.Asr.GetHistoryAudioLen(),
							)
							continue
						}

						if !hasLoggedFirstTextExtendedWait {
							log.Debugf(
								"realtime模式静音超时且仍未收到ASR文本，继续保持当前ASR流并转发音频: status=%s, idle=%dms, pre_text_timeout=%dms, voice_duration=%dms, voice_duration_in_session=%dms, history_audio_samples=%d",
								state.Status,
								idleDuration,
								preTextSilenceDuration,
								state.Vad.GetVoiceDuration(),
								voiceDurationInSession,
								state.Asr.GetHistoryAudioLen(),
							)
							hasLoggedFirstTextExtendedWait = true
						}
						continue
					}

					if state.IsSilence(idleDuration) { //从有声音到 静默的判断
						log.Debugf(
							"判定语音结束，准备停止ASR: status=%s, idle=%dms, voice_duration=%dms, voice_duration_in_session=%dms, history_audio_samples=%d, pending_restart=%v",
							state.Status,
							idleDuration,
							state.Vad.GetVoiceDuration(),
							state.Vad.GetVoiceDurationInSession(),
							state.Asr.GetHistoryAudioLen(),
							state.AudioIdleTimeoutPending(),
						)
						// 在 OnVoiceSilence 之前重置标志位，以便下次可以再次触发
						hasTriggeredCancel = false
						speakerInterruptTriggered.Store(false)
						lastSpeakerPeekDoneAt.Store(0)
						speakerPeekAudioMs = 0
						state.OnVoiceSilence()
						//state.VoiceStatus.Reset()
						continue
					}
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// releaseResource 释放ASR资源（内部方法）
func (a *ASRManager) releaseResource() {
	a.resourceMu.Lock()
	defer a.resourceMu.Unlock()
	if a.asrResource != nil {
		pool.Release(a.asrResource)
		a.asrResource = nil
		log.Debugf("ASR资源已归还")
	}
}

// Cleanup 清理ASR资源（供外部调用）
func (a *ASRManager) Cleanup() {
	a.releaseResource()
}

// restartAsrRecognition 重启ASR识别
func (a *ASRManager) RestartAsrRecognition(ctx context.Context) error {
	state := a.clientState
	log.Debugf("重启ASR识别开始")
	if a.session != nil {
		a.session.ResetTurnSpeakerInterrupted()
	}

	// 取消当前ASR上下文
	state.Asr.CancelWithReason("ASRManager.RestartAsrRecognition: cancel previous ASR context before restart")

	state.Asr.ResetReceivedText()
	state.VoiceStatus.Reset()
	state.AsrAudioBuffer.ClearAsrAudioData()
	state.Asr.ClearHistoryAudio() // 清空历史音频缓存

	// 检查是否已有资源，如果没有则获取
	a.resourceMu.Lock()
	var asrProvider asr.AsrProvider
	if a.asrResource == nil {
		// 需要获取新资源
		a.resourceMu.Unlock()

		asrWrapper, err := pool.Acquire[asr.AsrProvider](
			"asr",
			state.DeviceConfig.Asr.Provider,
			state.DeviceConfig.Asr.Config,
		)
		if err != nil {
			log.Errorf("获取ASR资源失败: %v", err)
			return fmt.Errorf("获取ASR资源失败: %w", err)
		}

		// 保存资源引用到私有字段
		a.resourceMu.Lock()
		a.asrResource = asrWrapper
		asrProvider = asrWrapper.GetProvider()
		a.resourceMu.Unlock()
		log.Debugf("获取新的ASR资源")
	} else {
		// 复用现有资源
		asrProvider = a.asrResource.GetProvider()
		a.resourceMu.Unlock()
		log.Debugf("复用现有ASR资源")
	}

	// 重新创建ASR上下文和通道
	state.Asr.Ctx, state.Asr.Cancel = context.WithCancel(ctx)
	state.Asr.AsrAudioChannel = make(chan []float32, 100)

	// 重新启动流式识别
	asrResultChannel, err := asrProvider.StreamingRecognize(state.Asr.Ctx, state.Asr.AsrAudioChannel)
	if err != nil {
		// 识别失败，归还资源（因为资源可能已损坏）
		a.releaseResource()
		log.Errorf("重启ASR流式识别失败: %v", err)
		return fmt.Errorf("重启ASR流式识别失败: %w", err)
	}

	state.AsrResultChannel = asrResultChannel
	// 重置统计时间，用于计算本轮对话的整体耗时
	state.MarkTurnStart()
	if a.session != nil {
		a.session.TraceTurnStart(state.Asr.Ctx, state.Statistic.TurnStartTs)
	}
	log.Debugf("重启ASR识别成功")
	return nil
}

// StartAsrRecognitionLoop 启动ASR识别结果处理循环
// onMessageSave: 消息保存回调函数
// onError: 错误处理回调函数（如关闭会话）
func (a *ASRManager) StartAsrRecognitionLoop(
	ctx context.Context,
	onMessageSave AsrMessageSaveCallback,
	onError func(error),
) {
	state := a.clientState

	// 启动一个goroutine处理asr结果
	go func() {
		// 使用 defer 确保 goroutine 退出时释放 ASR 资源
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("asr结果处理goroutine panic: %v, stack: %s", r, string(debug.Stack()))
			}
			// 无论正常退出还是 panic，都释放资源
			a.releaseResource()
		}()

		//最大空闲 60s
		var startIdleTime, maxIdleTime int64
		startIdleTime = time.Now().Unix()
		maxIdleTime = 60

		// 状态不允许重启时的等待计数（避免无限循环）
		var invalidStatusWaitCount int64
		maxInvalidStatusWaitCount := int64(10) // 最多等待10次（约1秒）

		// 空结果短时保护：避免 ASR 服务异常持续返回空字符串导致主流程死循环
		const emptyResultProtectWindow = 3 * time.Second
		const maxEmptyResultInWindow = 3
		emptyResultWindowStart := time.Now()
		emptyResultCount := 0

		// 可恢复错误短时保护：避免上游持续返回实例失效时无限重连
		const recoverableErrorProtectWindow = 10 * time.Second
		const maxRecoverableErrorInWindow = 3
		recoverableErrorWindowStart := time.Now()
		recoverableErrorCount := 0

		isAllowedToRestart := func() bool {
			allowed := state.Status == ClientStatusListening || state.Status == ClientStatusListenStop
			if state.IsRealTime() {
				allowed = state.Status != ClientStatusInit
			}
			return allowed
		}
		resumeAudioIdle := func() {
			state.ResumeAudioIdleWindow(time.Now())
		}
		startAudioIdle := func() {
			state.StartAudioIdleWindow(time.Now())
		}
		closeAudioIdleTimeout := func(reason string) {
			if !state.AudioIdleTimeoutPending() {
				return
			}

			state.ClearAudioIdleTimeoutPending()
			log.Infof("音频空闲超时收口完成: device=%s, reason=%s", state.DeviceID, reason)
			if a.session != nil {
				a.session.CloseWithReason(chatSessionCloseReasonAudioIdleTimeout)
				return
			}
			if onError != nil {
				onError(fmt.Errorf("audio idle timeout: %s", reason))
			}
		}

		for {
			select {
			case <-ctx.Done():
				log.Debugf("asr ctx done")
				return
			default:
			}

			result, isRetry, err := state.RetireAsrResult(ctx)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, context.Canceled) {
					log.Debugf("处理asr结果失败，ASR已取消: %v", err)
				} else {
					log.Errorf("处理asr结果失败: %v", err)
				}
				if onError != nil {
					onError(err)
				}
				return
			}
			if !isRetry {
				log.Debugf("asrResult is not retry, return")
				return
			}
			text := result.Text

			if result.RetryReason != "" {
				if state.AudioIdleTimeoutPending() {
					closeAudioIdleTimeout(result.RetryReason)
					return
				}

				now := time.Now()
				if now.Sub(recoverableErrorWindowStart) > recoverableErrorProtectWindow {
					recoverableErrorWindowStart = now
					recoverableErrorCount = 0
				}
				recoverableErrorCount++
				log.Warnf(
					"ASR可恢复错误: reason=%s, count=%d/%d, status=%s",
					result.RetryReason,
					recoverableErrorCount,
					maxRecoverableErrorInWindow,
					state.Status,
				)

				if recoverableErrorCount >= maxRecoverableErrorInWindow {
					err := fmt.Errorf("ASR短时间内连续触发可恢复错误(%d次/%s)，停止重试并断开连接", recoverableErrorCount, recoverableErrorProtectWindow)
					log.Errorf("%v", err)
					if onError != nil {
						onError(err)
					}
					return
				}

				switch result.RetryReason {
				case asr_types.RetryReasonDoubaoResponseCode45000081, asr_types.RetryReasonXunfeiServiceInstanceInvalid, asr_types.RetryReasonAliyunQwen3ConnectionClosed:
					a.releaseResource()
					if isAllowedToRestart() {
						invalidStatusWaitCount = 0
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("ASR可恢复错误后重启识别失败: reason=%s, err=%v", result.RetryReason, restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						resumeAudioIdle()
						continue
					}

					log.Warnf("ASR可恢复错误发生时当前状态不允许立即重启: reason=%s, status=%s, realtime=%v", result.RetryReason, state.Status, state.IsRealTime())
					state.Asr.CancelWithReason("ASRManager.StartAsrRecognitionLoop: recoverable error but restart not allowed yet")
					resumeAudioIdle()
					continue
				case asr_types.RetryReasonDoubaoWaitingNextPacketTimeout:
					log.Warnf("doubao ASR 会话空闲超时，挂起当前流并等待下一次语音时重建")
					state.Asr.CancelWithReason("ASRManager.StartAsrRecognitionLoop: doubao waiting next packet timeout")
					resumeAudioIdle()
					continue
				}
			}

			if text != "" {
				voiceDurationMs := state.Vad.GetVoiceDurationInSession()
				asrFinalTs := time.Now().UnixMilli()
				state.MarkAsrFinalTextAt(asrFinalTs)
				if a.session != nil {
					a.session.TraceAsrFinalText(ctx, asrFinalTs)
				}
				log.Debugf("处理asr结果: %s, 耗时: %d ms", text, state.GetAsrDuration())

				state.ClearAudioIdleTimeoutPending()
				// 识别成功后重置空结果计数
				emptyResultWindowStart = time.Now()
				emptyResultCount = 0
				recoverableErrorWindowStart = time.Now()
				recoverableErrorCount = 0

				// 重置重试计数器
				startIdleTime = time.Now().Unix()

				//当获取到asr结果时, 结束语音输入（OnVoiceSilence 中会异步获取声纹结果）
				state.OnVoiceSilence()

				// 获取暂存的声纹结果（带超时）
				speakerResult := a.getSpeakerResult()
				speakerInterrupted := false
				if a.session != nil {
					speakerInterrupted = a.session.ConsumeTurnSpeakerInterrupted()
				}

				if a.session != nil {
					payload, stop, hookErr := a.session.hookHub.EmitASROutput(a.session.hookContext(ctx), chathooks.ASROutputData{Text: text, SpeakerResult: speakerResult, VoiceDurationMs: voiceDurationMs})
					if hookErr != nil {
						log.Warnf("ASR_OUTPUT hook 执行失败: %v", hookErr)
					}
					text = payload.Text
					speakerResult = payload.SpeakerResult
					if stop {
						log.Infof("ASR_OUTPUT hook 请求停止当前流程")
						state.Asr.ClearHistoryAudio()

						if !state.IsRealTime() {
							if state.UsesAudioIdleClock() {
								startAudioIdle()
							} else {
								state.ResetAudioIdleWindow()
							}
							return
						}

						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("ASR_OUTPUT hook 停止流程后重启识别失败: %v", restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						startAudioIdle()
						continue
					}
				}

				if a.session != nil {
					allowChat, denyReason := a.session.ShouldAllowSpeakerChat(speakerResult, speakerInterrupted)
					if !allowChat {
						log.Infof(
							"丢弃ASR结果并跳过STT/LLM: device=%s, reason=%s, speaker_interrupted=%v, speaker_result=%+v, text=%q",
							state.DeviceID,
							denyReason,
							speakerInterrupted,
							speakerResult,
							text,
						)
						state.Asr.ClearHistoryAudio()

						if !state.IsRealTime() {
							startAudioIdle()
							return
						}
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("丢弃ASR结果后重启识别失败: %v", restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						startAudioIdle()
						continue
					}
				}

				//如果是realtime模式下，需要停止 当前的llm和tts
				if state.IsRealTime() && viper.GetInt("chat.realtime_mode") == 2 {
					shouldInterrupt := true
					if a.session != nil && a.session.isRealtimeMcpAudioGateActive() {
						shouldInterrupt = false
						log.Debugf("设备 %s realtime媒体播放门控激活，延后到ASR final门控判定，跳过ASR结果打断", state.DeviceID)
					}
					if shouldInterrupt {
						log.Debugf("OnListenStart realtime模式下, 停止当前的llm和tts")
						if a.session != nil {
							a.session.StopAssistantOutputAfterAsrWithReason(true, "ASRManager.StartAsrRecognitionLoop realtime_mode=2 ASR result interrupt")
						} else {
							state.AfterAsrSessionCtx.CancelWithReason("ASRManager.StartAsrRecognitionLoop: realtime_mode=2 ASR result interrupt")
						}
					}
				}

				// 创建用户消息，使用 hook 改写后的文本进入后续副作用链
				userMsg := &schema.Message{
					Role:    schema.User,
					Content: text,
				}

				// 生成 MessageID（使用 MD5 哈希缩短长度，避免超过数据库 varchar(64) 限制）
				// 原始格式：{SessionID}-{Role}-{Timestamp}
				rawMessageID := fmt.Sprintf("%s-%s-%d",
					state.SessionID,
					userMsg.Role,
					time.Now().UnixMilli())
				// 使用 MD5 哈希生成固定32字符的十六进制字符串
				hash := md5.Sum([]byte(rawMessageID))
				messageID := hex.EncodeToString(hash[:])

				// 同步添加到内存中（用于 LLM 上下文）
				state.AddMessage(userMsg)

				// 获取音频数据（ASR 历史音频）
				audioData := state.Asr.GetHistoryAudio()
				state.Asr.ClearHistoryAudio()

				// 通过回调保存消息
				if onMessageSave != nil {
					onMessageSave(userMsg, messageID, audioData)
				}

				// 发送给客户端的 ASR 结果也使用 hook 改写后的文本
				err = a.serverTransport.SendAsrResult(text)
				if err != nil {
					log.Errorf("发送asr消息失败: %v", err)
					if onError != nil {
						onError(err)
					}
					return
				}

				if a.session != nil {
					handledByRealtimeGate, gateErr := a.session.tryHandleRealtimeMcpAudioASR(ctx, text)
					if gateErr != nil {
						log.Warnf("realtime媒体播放快速控制失败: device=%s text=%q err=%v", state.DeviceID, text, gateErr)
					}
					if handledByRealtimeGate {
						if !state.IsRealTime() {
							return
						}
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("realtime媒体控制后重启ASR识别失败: %v", restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						startAudioIdle()
						continue
					}
				}

				// 添加到队列（迁移到 ASRManager 中处理）
				if err := a.addAsrResultToQueue(text, speakerResult); err != nil {
					log.Errorf("开始对话失败: %v", err)
					if onError != nil {
						onError(err)
					}
					return
				}

				// 非 realtime 模式下，ASR 识别完成，归还资源
				// realtime 模式下，资源会在 RestartAsrRecognition 中自动管理（先归还旧资源再获取新资源）
				if !state.IsRealTime() {
					return
				}

				// realtime 模式下，重启 ASR 识别（RestartAsrRecognition 会先归还旧资源再获取新资源）
				if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
					log.Errorf("重启ASR识别失败: %v", restartErr)
					if onError != nil {
						onError(restartErr)
					}
					return
				}
				// realtime模式下, 继续循环处理下一个 ASR 结果
				continue
			} else {
				log.Debugf(
					"ASR空结果详情: status=%s, emptyReason=%s, client_voice_stop=%v, history_audio_samples=%d, voice_duration=%dms, voice_duration_in_session=%dms, idle_duration=%dms, realtime=%v",
					state.Status,
					result.EmptyReason,
					state.GetClientVoiceStop(),
					state.Asr.GetHistoryAudioLen(),
					state.Vad.GetVoiceDuration(),
					state.Vad.GetVoiceDurationInSession(),
					state.Vad.GetIdleDuration(),
					state.IsRealTime(),
				)
				if state.AudioIdleTimeoutPending() {
					closeAudioIdleTimeout(result.EmptyReason)
					return
				}
				if result.EmptyReason != "" {
					log.Debugf("ASR空结果已分类: reason=%s, status=%s", result.EmptyReason, state.Status)
					emptyResultWindowStart = time.Now()
					emptyResultCount = 0

					if result.EmptyReason == asr_types.EmptyReasonNoServerResponse ||
						result.EmptyReason == asr_types.EmptyReasonProviderEmptyFinal {
						state.Asr.CancelWithReason("ASRManager.StartAsrRecognitionLoop: empty final result from provider")
						resumeAudioIdle()
						continue
					}
				}

				now := time.Now()
				if now.Sub(emptyResultWindowStart) > emptyResultProtectWindow {
					emptyResultWindowStart = now
					emptyResultCount = 0
				}
				emptyResultCount++
				if emptyResultCount >= maxEmptyResultInWindow {
					err := fmt.Errorf("ASR短时间内连续返回空结果(%d次/%s)，触发保护并断开连接", emptyResultCount, emptyResultProtectWindow)
					log.Errorf("%v", err)
					if onError != nil {
						onError(err)
					}
					return
				}

				// text 为空的情况
				select {
				case <-ctx.Done():
					log.Debugf("asr ctx done")
					return
				default:
				}

				log.Debugf("ready Restart Asr, state.Status: %s", state.Status)
				// realtime 模式下，即使状态是 LLMStart 或 TTSStart，也应该继续监听（允许重启ASR）
				// 非 realtime 模式下，只有 Listening 或 ListenStop 状态才允许重启ASR
				if isAllowedToRestart() {
					// 状态允许重启，重置等待计数
					invalidStatusWaitCount = 0
					// text 为空，检查是否需要重新启动ASR
					diffTs := time.Now().Unix() - startIdleTime
					if startIdleTime > 0 && diffTs <= maxIdleTime {
						log.Warnf("ASR识别结果为空，尝试重启ASR识别, diff ts: %d", diffTs)
						if restartErr := a.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("重启ASR识别失败: %v", restartErr)
							if onError != nil {
								onError(restartErr)
							}
							return
						}
						resumeAudioIdle()
						continue
					} else {
						log.Warnf("ASR识别结果为空，已达到最大空闲时间: %d", maxIdleTime)
						if onError != nil {
							onError(fmt.Errorf("ASR识别结果为空，已达到最大空闲时间: %d", maxIdleTime))
						}
						return
					}
				} else {
					// 状态不允许重启的情况，短暂等待后继续循环，给状态恢复的机会
					invalidStatusWaitCount++
					if invalidStatusWaitCount >= maxInvalidStatusWaitCount {
						// 等待超时，退出循环
						log.Debugf("状态为 %s，realtime: %v，等待%d次后仍无变化，退出ASR识别循环", state.Status, state.IsRealTime(), maxInvalidStatusWaitCount)
						return
					}
					// 短暂等待后继续循环，等待状态恢复
					log.Debugf("状态为 %s，realtime: %v，不允许重启，等待状态恢复 (等待次数: %d/%d)", state.Status, state.IsRealTime(), invalidStatusWaitCount, maxInvalidStatusWaitCount)
					time.Sleep(200 * time.Millisecond) // 等待100ms
					continue
				}
			}
		}
	}()
}

func trimFirstSpeechAudio(allData []float32, currentFrameSamples, sampleRate, channels int) []float32 {
	if len(allData) == 0 {
		return nil
	}
	if currentFrameSamples <= 0 || currentFrameSamples > len(allData) || sampleRate <= 0 || channels <= 0 {
		return allData
	}

	maxPreSpeechSamples := sampleRate * channels * maxFirstSpeechPreAudioMs / 1000
	keepSamples := currentFrameSamples + maxPreSpeechSamples
	if keepSamples >= len(allData) {
		return allData
	}

	audio := make([]float32, keepSamples)
	copy(audio, allData[len(allData)-keepSamples:])
	return audio
}

// getSpeakerResult 获取暂存的声纹结果（带超时）
func (a *ASRManager) getSpeakerResult() *speaker.IdentifyResult {
	if a.session == nil || a.session.speakerManager == nil {
		return nil
	}

	log.Debugf("speakerManager: %+v, IsActive: %+v", a.session.speakerManager, a.session.speakerManager.IsActive())

	timeout := time.NewTimer(200 * time.Millisecond)
	defer timeout.Stop()

	var speakerResult *speaker.IdentifyResult
	select {
	case <-a.session.speakerResultReady:
		a.session.speakerResultMu.RLock()
		speakerResult = a.session.pendingSpeakerResult
		a.session.speakerResultMu.RUnlock()
	case <-timeout.C:
		// 超时后读取当前结果（可能为 nil）
		a.session.speakerResultMu.RLock()
		speakerResult = a.session.pendingSpeakerResult
		a.session.speakerResultMu.RUnlock()
		log.Debugf("获取声纹识别结果超时，使用当前结果")
	}
	log.Debugf("获取声纹识别结果: %+v", speakerResult)
	return speakerResult
}

// addAsrResultToQueue 添加ASR结果到队列（迁移到 ASRManager 中处理）
func (a *ASRManager) addAsrResultToQueue(text string, speakerResult *speaker.IdentifyResult) error {
	if a.session == nil {
		return fmt.Errorf("session is nil")
	}
	return a.session.AddAsrResultToQueue(text, speakerResult)
}
