package chat

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	"xiaozhi-esp32-server-golang/constants"
	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	types_audio "xiaozhi-esp32-server-golang/internal/data/audio"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	userconfig "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/vad/silero_vad"
	log "xiaozhi-esp32-server-golang/logger"
)

type ChatManager struct {
	DeviceID  string
	transport types_conn.IConn

	clientState *ClientState
	session     *ChatSession
	ctx         context.Context
	cancel      context.CancelFunc
}

type ChatManagerOption func(*ChatManager)

func NewChatManager(deviceID string, transport types_conn.IConn, options ...ChatManagerOption) (*ChatManager, error) {

	cm := &ChatManager{
		DeviceID:  deviceID,
		transport: transport,
	}

	for _, option := range options {
		option(cm)
	}

	ctx := context.WithValue(context.Background(), "chat_session_operator", ChatSessionOperator(cm))

	cm.ctx, cm.cancel = context.WithCancel(ctx)

	// 先创建 clientState，再注册 OnClose 回调，避免竞态条件
	clientState, err := GenClientState(cm.ctx, cm.DeviceID)
	if err != nil {
		log.Errorf("初始化客户端状态失败: %v", err)
		cm.transport.Close()
		return nil, err
	}
	cm.clientState = clientState

	// clientState 创建完成后再注册 OnClose 回调
	cm.transport.OnClose(cm.OnClose)

	serverTransport := NewServerTransport(cm.transport, clientState)

	cm.session = NewChatSession(
		clientState,
		serverTransport,
	)

	return cm, nil
}

func GenClientState(pctx context.Context, deviceID string) (*ClientState, error) {
	configProvider, err := userconfig.GetProvider(viper.GetString("config_provider.type"))
	if err != nil {
		log.Errorf("获取 用户配置提供者失败: %+v", err)
		return nil, err
	}
	deviceConfig, err := configProvider.GetUserConfig(pctx, deviceID)
	if err != nil {
		log.Errorf("获取 设备 %s 配置失败: %+v", deviceID, err)
		return nil, err
	}

	if deviceConfig.Vad.Provider == "silero_vad" {
		silero_vad.InitVadPool(deviceConfig.Vad.Config)
	}

	// 创建带取消功能的上下文
	ctx, cancel := context.WithCancel(pctx)

	maxSilenceDuration := viper.GetInt64("chat.chat_max_silence_duration")
	if maxSilenceDuration == 0 {
		maxSilenceDuration = 200
	}

	isDeviceActivated, err := configProvider.IsDeviceActivated(ctx, deviceID, "")
	if err != nil {
		log.Errorf("检查设备激活状态失败: %v", err)
	}
	opusAudioBufferReader, opusAudioBufferWriter := schema.Pipe[[]byte](100)

	clientState := &ClientState{
		IsActivated:  isDeviceActivated,
		Dialogue:     &Dialogue{},
		Abort:        false,
		ListenMode:   "auto",
		DeviceID:     deviceID,
		AgentID:      deviceConfig.AgentId,
		Ctx:          ctx,
		Cancel:       cancel,
		SystemPrompt: deviceConfig.SystemPrompt,
		DeviceConfig: deviceConfig,
		OutputAudioFormat: types_audio.AudioFormat{
			SampleRate:    types_audio.SampleRate,
			Channels:      types_audio.Channels,
			FrameDuration: types_audio.FrameDuration,
			Format:        types_audio.Format,
		},
		OpusAudioBufferWriter: opusAudioBufferWriter,
		OpusAudioBufferReader: opusAudioBufferReader,
		AsrAudioBuffer: &AsrAudioBuffer{
			PcmData:          make([]float32, 0),
			AudioBufferMutex: sync.RWMutex{},
			PcmFrameSize:     0,
		},
		VoiceStatus: VoiceStatus{
			HaveVoice:            false,
			HaveVoiceLastTime:    0,
			VoiceStop:            false,
			SilenceThresholdTime: maxSilenceDuration,
		},
		SessionCtx: Ctx{},
	}

	ttsType := clientState.DeviceConfig.Tts.Provider
	//如果使用 xiaozhi tts，则固定使用24000hz, 20ms帧长
	if ttsType == constants.TtsTypeXiaozhi || ttsType == constants.TtsTypeEdgeOffline {
		clientState.OutputAudioFormat.SampleRate = 24000
		clientState.OutputAudioFormat.FrameDuration = 20
	}

	return clientState, nil
}

func (c *ChatManager) Start() error {
	err := c.session.Start(c.ctx)
	if err != nil {
		log.Errorf("ChatManager启动失败: %v", err)
		return err
	}
	select {
	case <-c.ctx.Done():
	}
	return nil
}

// 主动关闭断开连接
func (c *ChatManager) Close() error {
	if c.clientState != nil {
		log.Infof("主动关闭断开连接, 设备 %s", c.clientState.DeviceID)
	}
	// 先关闭会话级别的资源
	if c.session != nil {
		c.session.Close()
	}

	// 最后取消管理器级别的上下文
	c.cancel()

	return nil
}

func (c *ChatManager) OnClose(deviceId string) {
	log.Infof("设备 %s 断开连接", deviceId)
	c.cancel()
	if c.clientState != nil {
		eventbus.Get().Publish(eventbus.TopicSessionEnd, c.clientState)
	}
	return
}

func (c *ChatManager) GetClientState() *ClientState {
	return c.clientState
}

func (c *ChatManager) GetDeviceId() string {
	return c.clientState.DeviceID
}

// InjectMessage 注入消息到设备
func (c *ChatManager) InjectMessage(message string, skipLlm bool) error {
	if skipLlm {
		// 直接发送文本消息到设备，跳过LLM处理
		return c.session.AddTextToTTSQueue(message)
	} else {
		// 通过LLM处理消息
		return c.session.AddAsrResultToQueue(message)
	}
}
