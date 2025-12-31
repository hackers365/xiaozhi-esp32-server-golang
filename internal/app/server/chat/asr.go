package chat

import (
	"context"
	"fmt"
	"io"
	"time"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	vad_types "xiaozhi-esp32-server-golang/internal/data/vad"
	"xiaozhi-esp32-server-golang/internal/domain/audio"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

type ASRManagerOption func(*ASRManager)

type ASRManager struct {
	clientState     *ClientState
	serverTransport *ServerTransport
}

func NewASRManager(clientState *ClientState, serverTransport *ServerTransport, opts ...ASRManagerOption) *ASRManager {
	asr := &ASRManager{
		clientState:     clientState,
		serverTransport: serverTransport,
	}
	for _, opt := range opts {
		opt(asr)
	}
	return asr
}

type Option struct {
}

// ProcessVadAudio 启动VAD音频处理
func (a *ASRManager) ProcessVadAudio(
	ctx context.Context,
	input *schema.StreamReader[[]byte],
	opts ...vad_types.Option) (*schema.StreamReader[[]float32], error) {

	state := a.clientState
	vadOpts := vad_types.GetImplSpecificOptions(&vad_types.Options{}, opts...)
	onClose := vadOpts.OnClose

	outputReader, outputWriter := schema.Pipe[[]float32](100)
	go func() {
		defer outputWriter.Close()
		audioFormat := state.InputAudioFormat
		audioProcesser, err := audio.GetAudioProcesser(audioFormat.SampleRate, audioFormat.Channels, audioFormat.FrameDuration)
		if err != nil {
			log.Errorf("获取解码器失败: %v", err)
			return
		}
		frameSize := state.AsrAudioBuffer.PcmFrameSize

		vadNeedGetCount := 1
		if state.DeviceConfig.Vad.Provider == "silero_vad" {
			vadNeedGetCount = 60 / audioFormat.FrameDuration
		}

		for {
			pcmFrame := make([]float32, frameSize)

			audioData, err := input.Recv()
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Errorf("读取音频数据失败: %v", err)
				return
			}
			opusFrame := audioData
			var skipVad bool
			var haveVoice bool
			clientHaveVoice := state.GetClientHaveVoice()
			if state.Asr.AutoEnd || state.ListenMode == "manual" {
				skipVad = true         //跳过vad
				clientHaveVoice = true //之前有声音
				haveVoice = true       //本次有声音
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

			var vadPcmData []float32
			pcmData := pcmFrame[:n]
			if !skipVad {
				//decode opus to pcm
				state.AsrAudioBuffer.AddAsrAudioData(pcmData)

				if state.AsrAudioBuffer.GetAsrDataSize() >= vadNeedGetCount*state.AsrAudioBuffer.PcmFrameSize {
					//如果要进行vad, 至少要取60ms的音频数据
					vadPcmData = state.AsrAudioBuffer.GetAsrData(vadNeedGetCount)

					//如果已经检测到语音, 则不进行vad检测, 直接将pcmData传给asr
					if state.Vad.VadProvider == nil {
						// 初始化vad
						err = state.Vad.Init(state.DeviceConfig.Vad.Provider, state.DeviceConfig.Vad.Config)
						if err != nil {
							log.Errorf("初始化vad失败: %v", err)
							continue
						}
					}
					err = state.Vad.ResetVad()
					if err != nil {
						log.Errorf("重置vad失败: %v", err)
						continue
					}
					haveVoice, err = state.Vad.IsVADExt(vadPcmData, audioFormat.SampleRate, frameSize)
					if err != nil {
						log.Errorf("processAsrAudio VAD检测失败: %v", err)
						//删除
						continue
					}
					//首次触发识别到语音时,为了语音数据完整性 将vadPcmData赋值给pcmData, 之后的音频数据全部进入asr
					if haveVoice && !clientHaveVoice {
						if state.IsRealTime() {
							//realtime模式下, 如果此时有正在进行的llm和tts则取消掉
							state.AfterAsrSessionCtx.Cancel()
						}
						//首次获取全部pcm数据送入asr
						pcmData = state.AsrAudioBuffer.GetAndClearAllData()
					}
				}
				//log.Debugf("isVad, pcmData len: %d, vadPcmData len: %d, haveVoice: %v", len(pcmData), len(vadPcmData), haveVoice)
			}

			if !haveVoice || state.Asr.AutoEnd {
				state.Vad.AddIdleDuration(int64(audioFormat.FrameDuration))
				idleDuration := state.Vad.GetIdleDuration()
				log.Infof("空闲时间: %dms", idleDuration)
				if idleDuration > state.GetMaxIdleDuration() {
					log.Infof("超出空闲时长: %dms, 断开连接", idleDuration)
					//断开连接
					onClose()
					return
				}
			}

			if haveVoice {
				//log.Infof("检测到语音, len: %d", len(pcmData))
				state.SetClientHaveVoice(true)
				state.SetClientHaveVoiceLastTime(time.Now().UnixMilli())
				if !state.Asr.AutoEnd {
					state.Vad.ResetIdleDuration()
				}

				// 累积检测到声音的时长
				state.Vad.AddVoiceDuration(int64(audioFormat.FrameDuration))

				voiceDuration := state.Vad.GetVoiceDuration()
				log.Debugf("voiceDuration: %d", voiceDuration)
				if state.IsRealTime() && viper.GetInt("chat.realtime_mode") == 1 && voiceDuration > 120 {
					//realtime模式下, 如果此时有正在进行的llm和tts则取消掉
					log.Debugf("realtime模式vad打断下 && 语音时长超过%d ms 如果此时有正在进行的llm和tts则取消掉", voiceDuration)
					state.AfterAsrSessionCtx.Cancel()
				}
			} else {
				state.Vad.ResetVoiceDuration()
				//如果之前没有语音, 本次也没有语音, 则从缓存中删除
				if !clientHaveVoice {
					//保留近10帧
					if state.AsrAudioBuffer.GetFrameCount() > vadNeedGetCount*3 {
						state.AsrAudioBuffer.RemoveAsrAudioData(1)
					}
					continue
				}
			}

			if clientHaveVoice {
				//vad识别成功, 往asr音频通道里发送数据
				//log.Infof("vad识别成功, 往asr音频通道里发送数据, len: %d", len(pcmData))
				//state.Asr.AddAudioData(pcmData)
				closed := outputWriter.Send(pcmData, nil)
				if closed {
					log.Errorf("asr音频通道已关闭, 停止发送数据")
					return
				}
			}

			//已经有语音了, 但本次没有检测到语音, 则需要判断是否已经停止说话
			lastHaveVoiceTime := state.GetClientHaveVoiceLastTime()

			if clientHaveVoice && lastHaveVoiceTime > 0 && !haveVoice {
				idleDuration := state.Vad.GetIdleDuration()
				if state.IsSilence(idleDuration) { //从有声音到 静默的判断
					state.OnVoiceSilence()
					return
				}
			}
		}
	}()
	return outputReader, nil
}

// restartAsrRecognition 重启ASR识别
func (a *ASRManager) RestartAsrRecognition(ctx context.Context, audioStream *schema.StreamReader[[]float32]) error {
	state := a.clientState
	log.Debugf("重启ASR识别开始")

	// 取消当前ASR上下文
	if state.Asr.Cancel != nil {
		state.Asr.Cancel()
	}

	state.VoiceStatus.Reset()
	state.AsrAudioBuffer.ClearAsrAudioData()

	// 等待一小段时间让资源清理
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// 重新创建ASR上下文和通道
	state.Asr.Ctx, state.Asr.Cancel = context.WithCancel(ctx)
	state.Asr.AsrAudioChannel = make(chan []float32, 100)

	// 重新启动流式识别
	asrResultChannel, err := state.AsrProvider.StreamingRecognize(state.Asr.Ctx, audioStream)
	if err != nil {
		log.Errorf("重启ASR流式识别失败: %v", err)
		return fmt.Errorf("重启ASR流式识别失败: %v", err)
	}

	state.AsrResultChannel = asrResultChannel
	log.Debugf("重启ASR识别成功")
	return nil
}
