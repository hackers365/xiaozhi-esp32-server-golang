package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/viper"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/app/server/auth"
	"xiaozhi-esp32-server-golang/internal/app/server/chat/plugins"
	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	types_audio "xiaozhi-esp32-server-golang/internal/data/audio"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	. "xiaozhi-esp32-server-golang/internal/data/msg"
	chathooks "xiaozhi-esp32-server-golang/internal/domain/chat/hooks"
	"xiaozhi-esp32-server-golang/internal/domain/chat/streamtransform"
	userconfig "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/openclaw"
	pkghooks "xiaozhi-esp32-server-golang/internal/pkg/hooks"
	log "xiaozhi-esp32-server-golang/logger"
)

type ChatManager struct {
	DeviceID  string
	transport types_conn.IConn

	clientState       *ClientState
	serverTransport   *ServerTransport
	mcpTransport      *McpTransport
	hookHub           *chathooks.Hub
	transformRegistry *streamtransform.Registry

	sessionMu sync.RWMutex
	session   *ChatSession

	startingSession     *ChatSession
	startingSessionDone chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	helloMu      sync.Mutex
	helloInited  bool
	mcpInitState chatMcpInitState

	speakRequestMu      sync.Mutex
	pendingSpeakRequest *pendingSpeakRequest
	lastSpeakPathWarmAt atomic.Int64
	speakReadyTimeout   time.Duration

	// Close 保护，防止多次关闭
	closeOnce      sync.Once
	managerClosing atomic.Bool
	needFreshHello bool

	mqttRebootstrapPending atomic.Bool

	retainedSessionCleanupMu     sync.Mutex
	retainedSessionCleanupTimer  *time.Timer
	retainedSessionCleanupTarget *ChatSession
	retainedSessionIdleTimeout   time.Duration
}

type pendingSpeakRequest struct {
	sessionID string
	done      chan struct{}
	timer     *time.Timer

	once sync.Once
	mu   sync.Mutex
	err  error
}

func (p *pendingSpeakRequest) resolve(err error) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.timer != nil {
			p.timer.Stop()
		}
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
	})
}

func (p *pendingSpeakRequest) Err() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

type chatMcpInitState uint8

const (
	chatMcpInitStateIdle chatMcpInitState = iota
	chatMcpInitStateInFlight
	chatMcpInitStateReady
)

const (
	defaultSpeakRequestReuseWindow = 60 * time.Second
	defaultSpeakReadyTimeout       = 5 * time.Second
	defaultRetainedSessionIdleTTL  = 10 * time.Minute
)

type brokerOnlineAwareTransport interface {
	IsBrokerOnline() bool
}

type ChatManagerOption func(*ChatManager)

var (
	chatHookAsyncExecutorOnce sync.Once
	chatHookAsyncExecutor     *pkghooks.AsyncExecutor
)

func sharedChatHookAsyncExecutor() *pkghooks.AsyncExecutor {
	chatHookAsyncExecutorOnce.Do(func() {
		asyncCfg := pkghooks.AsyncConfig{
			QueueSize:    viper.GetInt("chat_hooks.async.queue_size"),
			WorkerCount:  viper.GetInt("chat_hooks.async.worker_count"),
			DropWhenFull: viper.GetBool("chat_hooks.async.drop_when_full"),
			Timeout:      time.Duration(viper.GetInt("chat_hooks.async.timeout_ms")) * time.Millisecond,
		}
		chatHookAsyncExecutor = pkghooks.NewAsyncExecutor(context.Background(), asyncCfg)
		log.Infof("初始化全局共享 chat hook observer executor: queue_size=%d worker_count=%d drop_when_full=%v timeout=%s", asyncCfg.QueueSize, asyncCfg.WorkerCount, asyncCfg.DropWhenFull, asyncCfg.Timeout)
	})
	return chatHookAsyncExecutor
}

func newChatHookHub(parent context.Context) *chathooks.Hub {
	asyncCfg := pkghooks.AsyncConfig{
		QueueSize:    viper.GetInt("chat_hooks.async.queue_size"),
		WorkerCount:  viper.GetInt("chat_hooks.async.worker_count"),
		DropWhenFull: viper.GetBool("chat_hooks.async.drop_when_full"),
		Timeout:      time.Duration(viper.GetInt("chat_hooks.async.timeout_ms")) * time.Millisecond,
	}
	hub := chathooks.NewHub(parent, pkghooks.WithAsyncConfig(asyncCfg), pkghooks.WithAsyncExecutor(sharedChatHookAsyncExecutor()))
	stats := hub.Stats()
	log.Infof("初始化 chat hook hub: queue_size=%d worker_count=%d drop_when_full=%v timeout=%s dropped_async=%d", asyncCfg.QueueSize, asyncCfg.WorkerCount, asyncCfg.DropWhenFull, asyncCfg.Timeout, stats.DroppedAsync)
	return hub
}

func chatHookBuiltinOverrides() map[string]chathooks.BuiltinPluginConfig {
	overrides := map[string]chathooks.BuiltinPluginConfig{}
	for _, reg := range chathooks.BuiltinRegistrations() {
		path := "chat_hooks.plugins." + reg.Meta.Name
		cfg := chathooks.BuiltinPluginConfig{}
		if viper.IsSet(path + ".enabled") {
			enabled := viper.GetBool(path + ".enabled")
			cfg.Enabled = &enabled
		}
		if viper.IsSet(path + ".priority") {
			cfg.Priority = viper.GetInt(path + ".priority")
		}
		overrides[reg.Meta.Name] = cfg
	}
	return overrides
}

func NewChatManager(deviceID string, transport types_conn.IConn, options ...ChatManagerOption) (*ChatManager, error) {
	cm := &ChatManager{
		DeviceID:          deviceID,
		transport:         transport,
		speakReadyTimeout: defaultSpeakReadyTimeout,
	}

	for _, option := range options {
		option(cm)
	}

	ctx := context.WithValue(context.Background(), "chat_session_operator", ChatSessionOperator(cm))
	ctx = withTTSTurnEndPolicyHandler(ctx, cm)
	cm.ctx, cm.cancel = context.WithCancel(ctx)

	clientState, err := GenClientState(cm.ctx, cm.DeviceID)
	if err != nil {
		log.Errorf("初始化客户端状态失败: %v", err)
		_ = cm.transport.Close()
		return nil, err
	}
	cm.clientState = clientState

	cm.serverTransport = NewServerTransport(cm.transport, clientState)
	cm.mcpTransport = &McpTransport{
		Client:          clientState,
		ServerTransport: cm.serverTransport,
	}

	cm.transport.OnClose(cm.OnClose)

	cm.hookHub = newChatHookHub(cm.ctx)
	if !viper.IsSet("chat_hooks.enabled") || viper.GetBool("chat_hooks.enabled") {
		if err := chathooks.RegisterBuiltinPlugins(cm.hookHub, chatHookBuiltinOverrides()); err != nil {
			log.Errorf("注册 chat hook builtin plugins 失败: %v", err)
			_ = cm.transport.Close()
			return nil, err
		}
		log.Infof("已加载 chat hook plugins: %+v", cm.hookHub.PluginMetas())
	}

	cm.transformRegistry = streamtransform.NewRegistry()
	plugins.Init(cm.transformRegistry)

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
	deviceConfig.MemoryMode = NormalizeMemoryMode(deviceConfig.MemoryMode)
	deviceConfig.SpeakerChatMode = NormalizeSpeakerChatMode(deviceConfig.SpeakerChatMode)

	ctx, cancel := context.WithCancel(pctx)

	maxSilenceDuration := viper.GetInt64("chat.chat_max_silence_duration")
	if !viper.IsSet("chat.chat_max_silence_duration") {
		maxSilenceDuration = 400
	}

	isDeviceActivated, err := configProvider.IsDeviceActivated(ctx, deviceID, "")
	if err != nil {
		log.Errorf("检查设备激活状态失败: %v", err)
	}

	clientState := &ClientState{
		IsActivated:       isDeviceActivated,
		Dialogue:          &Dialogue{},
		Abort:             false,
		ListenMode:        "auto",
		ListenPhase:       ListenPhaseIdle,
		DeviceID:          deviceID,
		AgentID:           deviceConfig.AgentId,
		Ctx:               ctx,
		Cancel:            cancel,
		SystemPrompt:      deviceConfig.SystemPrompt,
		DeviceConfig:      deviceConfig,
		OutputAudioFormat: types_audio.AudioFormat{},
		OpusAudioBuffer:   make(chan []byte, 100),
		AsrAudioBuffer: &AsrAudioBuffer{
			PcmData:          make([]float32, 0),
			AudioBufferMutex: sync.RWMutex{},
		},
		VoiceStatus: VoiceStatus{
			HaveVoice:            false,
			HaveVoiceLastTime:    0,
			VoiceStop:            false,
			SilenceThresholdTime: maxSilenceDuration,
		},
		SessionCtx: Ctx{},
	}
	applyOutputAudioFormatForTTS(clientState)

	return clientState, nil
}

func applyOutputAudioFormatForTTS(clientState *ClientState) {
	clientState.OutputAudioFormat = types_audio.AudioFormat{
		SampleRate:    types_audio.SampleRate,
		Channels:      types_audio.Channels,
		FrameDuration: types_audio.FrameDuration,
		Format:        types_audio.Format,
	}
	ttsType := clientState.DeviceConfig.Tts.Provider
	if ttsType == constants.TtsTypeXiaozhi {
		clientState.OutputAudioFormat.SampleRate = 24000
		clientState.OutputAudioFormat.FrameDuration = 20
	}
}

func (c *ChatManager) ReloadDeviceConfig(ctx context.Context) error {
	configProvider, err := userconfig.GetProvider(viper.GetString("config_provider.type"))
	if err != nil {
		return fmt.Errorf("获取配置提供者失败: %w", err)
	}

	deviceConfig, err := configProvider.GetUserConfig(ctx, c.DeviceID)
	if err != nil {
		return fmt.Errorf("获取设备配置失败: %w", err)
	}
	deviceConfig.MemoryMode = NormalizeMemoryMode(deviceConfig.MemoryMode)
	deviceConfig.SpeakerChatMode = NormalizeSpeakerChatMode(deviceConfig.SpeakerChatMode)

	oldAgentID := c.clientState.AgentID
	c.clientState.AgentID = deviceConfig.AgentId
	c.clientState.DeviceConfig = deviceConfig
	c.clientState.SystemPrompt = deviceConfig.SystemPrompt
	c.clientState.SpeakerTTSConfig = nil
	openclaw.GetManager().ExitMode(oldAgentID, c.DeviceID)
	openclaw.GetManager().ExitMode(c.clientState.AgentID, c.DeviceID)
	applyOutputAudioFormatForTTS(c.clientState)
	log.Infof("设备 %s 配置已刷新，当前agent=%s", c.DeviceID, deviceConfig.AgentId)
	return nil
}

func (c *ChatManager) Start() error {
	go c.cmdMessageLoop(c.ctx)
	go c.audioMessageLoop(c.ctx)

	<-c.ctx.Done()
	return nil
}

func (c *ChatManager) handleLoopExit(loopName string, ctx context.Context) {
	if r := recover(); r != nil {
		log.Errorf("设备 %s %s loop panic: %v\n%s", c.DeviceID, loopName, r, string(debug.Stack()))
	}
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if c.serverTransport != nil && c.serverTransport.IsClosed() {
		return
	}
	log.Warnf("设备 %s %s loop 异常退出，关闭 ChatManager", c.DeviceID, loopName)
	if err := c.Close(); err != nil {
		log.Warnf("设备 %s %s loop 退出后关闭 ChatManager 失败: %v", c.DeviceID, loopName, err)
	}
}

func (c *ChatManager) cmdMessageLoop(ctx context.Context) {
	defer c.handleLoopExit("cmd", ctx)

	recvFailCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Infof("设备 %s recvCmd context cancel", c.DeviceID)
			return
		default:
		}

		if recvFailCount > 3 {
			log.Errorf("设备 %s recv cmd timeout 超过阈值", c.DeviceID)
			return
		}

		message, err := c.serverTransport.RecvCmd(ctx, 120)
		if err != nil {
			log.Errorf("recv cmd error: %v", err)
			recvFailCount++
			continue
		}
		if message == nil {
			continue
		}

		recvFailCount = 0
		log.Infof("收到文本消息: %s", string(message))
		if err := c.handleTextMessage(message); err != nil {
			log.Errorf("处理文本消息失败: %v, 消息内容: %s", err, string(message))
		}
	}
}

func (c *ChatManager) audioMessageLoop(ctx context.Context) {
	defer c.handleLoopExit("audio", ctx)

	for {
		select {
		case <-ctx.Done():
			log.Debugf("设备 %s recvAudio context cancel", c.DeviceID)
			return
		default:
		}

		message, err := c.serverTransport.RecvAudio(ctx, 600)
		if err != nil {
			log.Errorf("recv audio error: %v", err)
			return
		}
		if message == nil {
			continue
		}

		session := c.GetSession()
		if session == nil {
			log.Debugf("设备 %s 当前无活动 ChatSession，丢弃音频数据", c.DeviceID)
			continue
		}
		if c.hasPendingSpeakRequest() {
			log.Debugf("设备 %s 当前存在待完成 speak_request，丢弃音频数据", c.DeviceID)
			continue
		}

		log.Debugf("收到音频数据，大小: %d 字节", len(message))
		isAuth := viper.GetBool("auth.enable")
		if isAuth && !c.clientState.IsActivated {
			log.Debugf("设备 %s 未激活, 跳过音频数据", c.clientState.DeviceID)
			continue
		}
		if c.clientState.GetClientVoiceStop() {
			log.Debug("客户端停止说话, 跳过音频数据")
			continue
		}

		if ok := session.HandleAudioMessage(message); !ok {
			log.Warnf("音频缓冲区已满，丢弃音频数据")
		}
	}
}

func (c *ChatManager) handleTextMessage(message []byte) error {
	var clientMsg ClientMessage
	if err := json.Unmarshal(message, &clientMsg); err != nil {
		log.Errorf("解析消息失败: %v", err)
		return fmt.Errorf("解析消息失败: %v", err)
	}

	if clientMsg.Type != MessageTypeGoodBye {
		c.cancelRetainedSessionCleanup("收到设备活动消息")
	}

	switch clientMsg.Type {
	case MessageTypeHello:
		return c.HandleHelloMessage(&clientMsg)
	case MessageTypeSpeakReady:
		return c.HandleSpeakReadyMessage(&clientMsg)
	case MessageTypeListen:
		return c.HandleListenMessage(&clientMsg)
	case MessageTypeAbort:
		return c.HandleAbortMessage(&clientMsg)
	case MessageTypeIot:
		return c.HandleIoTMessage(&clientMsg)
	case MessageTypeMcp:
		return c.HandleMcpMessage(&clientMsg)
	case MessageTypeGoodBye:
		return c.HandleGoodByeMessage(&clientMsg)
	default:
		return fmt.Errorf("未知消息类型: %s", clientMsg.Type)
	}
}

func (c *ChatManager) HandleHelloMessage(msg *ClientMessage) error {
	if msg.AudioParams == nil {
		return fmt.Errorf("hello消息缺少audio_params")
	}
	transportType := strings.TrimSpace(msg.Transport)
	if transportType == "" && c.serverTransport != nil {
		transportType = c.serverTransport.GetTransportType()
	}
	switch transportType {
	case types_conn.TransportTypeWebsocket, types_conn.TransportTypeMqttUdp:
	default:
		return fmt.Errorf("不支持的传输类型: %s", transportType)
	}

	c.helloMu.Lock()
	defer c.helloMu.Unlock()

	clientState := c.clientState
	clientState.InputAudioFormat = *msg.AudioParams
	isFirstHello := !c.helloInited
	requiresFreshHello := c.requiresFreshHello()
	isDuplicateMqttHello := !isFirstHello && !requiresFreshHello &&
		c.serverTransport != nil && c.serverTransport.GetTransportType() == types_conn.TransportTypeMqttUdp
	if c.helloInited {
		prevAgentID := clientState.AgentID
		if err := c.refreshDeviceConfigOnHello(); err != nil {
			log.Warnf("设备 %s duplicate hello 刷新配置失败，降级继续: %v", clientState.DeviceID, err)
		}
		c.resetOpenClawModeOnHello(prevAgentID, clientState.AgentID)
	} else {
		c.resetOpenClawModeOnHello(clientState.AgentID)
	}

	if isFirstHello || requiresFreshHello {
		preferredSessionID := ""
		if isFirstHello {
			preferredSessionID = strings.TrimSpace(clientState.SessionID)
		}
		session, err := auth.A().EnsureSession(msg.DeviceID, preferredSessionID)
		if err != nil {
			return fmt.Errorf("创建会话失败: %v", err)
		}
		clientState.SessionID = session.ID
		c.helloInited = true
	}
	if isDuplicateMqttHello {
		log.Infof("设备 %s 收到 duplicate_hello，执行 hello 重协商", clientState.DeviceID)
		c.markMqttConversationStateStale("duplicate_hello")
	}

	chatSession, err := c.ensureSessionForHello()
	if err != nil {
		if isFirstHello || requiresFreshHello {
			c.setNeedFreshHello(true)
		}
		return err
	}
	if err := c.sendHelloResponse(msg); err != nil {
		if isFirstHello || requiresFreshHello {
			c.setNeedFreshHello(true)
			if chatSession != nil {
				chatSession.CloseWithReason(chatSessionCloseReasonFatalError)
			}
		}
		return err
	}
	c.refreshSpeakPathWarmFromTransport()
	c.scheduleMcpInitOnHelloLocked(msg)
	if !isFirstHello && !requiresFreshHello {
		log.Infof("设备 %s duplicate_hello 处理完成，hello 重协商已刷新", clientState.DeviceID)
	}
	return nil
}

func (c *ChatManager) scheduleMcpInitOnHelloLocked(msg *ClientMessage) {
	if !c.hasMcpFeature(msg) {
		return
	}
	c.scheduleMcpInitLocked()
}

func (c *ChatManager) scheduleMcpInitLocked() {
	if c.mcpTransport == nil {
		return
	}
	if c.mcpInitState == chatMcpInitStateInFlight {
		return
	}
	if !shouldScheduleDeviceMcpRuntimeInit(c.clientState.DeviceID, c.mcpTransport) {
		return
	}
	if c.mcpInitState == chatMcpInitStateReady {
		log.Warnf(
			"设备 %s MCP 状态漂移: ChatManager 已ready，但 transport=%s 需要重新初始化",
			c.DeviceID,
			strings.TrimSpace(c.mcpTransport.GetMcpTransportType()),
		)
	}

	c.mcpInitState = chatMcpInitStateInFlight
	deviceID := c.clientState.DeviceID
	transportType := strings.TrimSpace(c.mcpTransport.GetMcpTransportType())
	go func() {
		err := initMcp(deviceID, c.mcpTransport)
		c.finishMcpInit(transportType, err)
	}()
}

func (c *ChatManager) finishMcpInit(transportType string, err error) {
	c.helloMu.Lock()
	defer c.helloMu.Unlock()

	if c.ctx.Err() != nil || c.managerClosing.Load() {
		return
	}
	if c.mcpTransport == nil {
		c.mcpInitState = chatMcpInitStateIdle
		return
	}
	currentTransportType := strings.TrimSpace(c.mcpTransport.GetMcpTransportType())
	if currentTransportType != strings.TrimSpace(transportType) {
		return
	}

	if err != nil {
		c.mcpInitState = chatMcpInitStateIdle
		log.Warnf("设备 %s MCP 初始化失败，等待后续 hello 重试: %v", c.DeviceID, err)
		return
	}

	c.mcpInitState = chatMcpInitStateReady
}

func (c *ChatManager) hasMcpFeature(msg *ClientMessage) bool {
	if msg == nil || msg.Features == nil {
		return false
	}
	isMcp, ok := msg.Features["mcp"]
	return ok && isMcp
}

func (c *ChatManager) sendHelloResponse(msg *ClientMessage) error {
	transportType := strings.TrimSpace(msg.Transport)
	if transportType == "" {
		transportType = c.serverTransport.GetTransportType()
	}

	switch transportType {
	case types_conn.TransportTypeWebsocket:
		return c.serverTransport.SendHello(types_conn.TransportTypeWebsocket, &c.clientState.OutputAudioFormat, nil)
	case types_conn.TransportTypeMqttUdp:
		udpConfig, err := c.buildMqttHelloUdpConfig()
		if err != nil {
			return err
		}
		return c.serverTransport.SendHello(types_conn.TransportTypeMqttUdp, &c.clientState.OutputAudioFormat, udpConfig)
	default:
		return fmt.Errorf("不支持的传输类型: %s", transportType)
	}
}

func (c *ChatManager) buildMqttHelloUdpConfig() (*UdpConfig, error) {
	udpExternalHost := viper.GetString("udp.external_host")
	udpExternalPort := viper.GetInt("udp.external_port")

	aesKey, err := c.serverTransport.GetData("aes_key")
	if err != nil {
		return nil, fmt.Errorf("获取aes_key失败: %v", err)
	}
	fullNonce, err := c.serverTransport.GetData("full_nonce")
	if err != nil {
		return nil, fmt.Errorf("获取full_nonce失败: %v", err)
	}

	strAesKey, ok := aesKey.(string)
	if !ok {
		return nil, fmt.Errorf("aes_key不是字符串")
	}
	strFullNonce, ok := fullNonce.(string)
	if !ok {
		return nil, fmt.Errorf("full_nonce不是字符串")
	}

	return &UdpConfig{
		Server: udpExternalHost,
		Port:   udpExternalPort,
		Key:    strAesKey,
		Nonce:  strFullNonce,
	}, nil
}

func (c *ChatManager) HandleListenMessage(msg *ClientMessage) error {
	if c.requiresHelloBootstrapForSession() {
		log.Infof(
			"设备 %s 当前会话已关闭或尚未完成 hello，忽略 listen %s，等待新 hello",
			c.DeviceID,
			strings.TrimSpace(msg.State),
		)
		return nil
	}
	session, err := c.ensureSession()
	if err != nil {
		return err
	}
	c.clearMqttRebootstrapPending("listen_message")
	return session.HandleListenMessage(msg)
}

func (c *ChatManager) HandleAbortMessage(msg *ClientMessage) error {
	session := c.GetSession()
	if session == nil {
		log.Debugf("设备 %s 当前无活动 ChatSession，忽略 abort", c.DeviceID)
		return nil
	}
	return session.HandleAbortMessage(msg)
}

func (c *ChatManager) HandleIoTMessage(msg *ClientMessage) error {
	if err := c.serverTransport.SendIot(msg); err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}
	log.Infof("设备 %s 物联网指令: %s", msg.DeviceID, msg.Text)
	return nil
}

func (c *ChatManager) HandleMcpMessage(msg *ClientMessage) error {
	return mcp.HandleDeviceIotMcpMessage(c.clientState.DeviceID, c.mcpTransport.GetMcpTransportType(), msg.PayLoad)
}

func (c *ChatManager) HandleGoodByeMessage(msg *ClientMessage) error {
	session := c.GetSession()
	if session != nil {
		log.Infof("设备 %s 收到设备端 goodbye，保留 ChatSession 并重置为静默态", c.DeviceID)
		session.ResetToSilentState()
		c.scheduleRetainedSessionCleanup(session, "peer_goodbye")
	} else {
		log.Infof("设备 %s 收到设备端 goodbye，但当前无活动 ChatSession，仅清理音频链路", c.DeviceID)
	}

	c.resetSpeakPathAfterGoodbye()
	return c.transport.CloseAudioChannel()
}

func (c *ChatManager) HandleSpeakReadyMessage(msg *ClientMessage) error {
	if msg == nil {
		return nil
	}
	if c.serverTransport == nil || c.serverTransport.GetTransportType() != types_conn.TransportTypeMqttUdp {
		return nil
	}
	if msg.State != "" && msg.State != MessageStateReady {
		log.Debugf("设备 %s speak_ready 状态不是 ready，忽略: %+v", c.DeviceID, msg)
		return nil
	}
	if msg.SpeakUDPConfig != nil && !msg.SpeakUDPConfig.Ready {
		log.Warnf("设备 %s speak_ready udp_config.ready=false，忽略", c.DeviceID)
		return nil
	}

	c.speakRequestMu.Lock()
	pending := c.pendingSpeakRequest
	c.speakRequestMu.Unlock()
	if pending == nil {
		log.Debugf("设备 %s 收到无待处理请求的 speak_ready，忽略", c.DeviceID)
		return nil
	}
	if pending.sessionID != "" && strings.TrimSpace(msg.SessionID) != pending.sessionID {
		log.Warnf("设备 %s speak_ready session_id 不匹配: got=%s want=%s", c.DeviceID, msg.SessionID, pending.sessionID)
		return nil
	}

	c.markSpeakPathWarm(time.Now())
	c.clearMqttRebootstrapPending("speak_ready")
	c.finishPendingSpeakRequest(pending, nil)

	reuseExisting := false
	if msg.SpeakUDPConfig != nil {
		reuseExisting = msg.SpeakUDPConfig.ReuseExisting
	}
	log.Infof("设备 %s speak_ready 已就绪，reuse_existing=%v", c.DeviceID, reuseExisting)
	return nil
}

func (c *ChatManager) ensureSession() (*ChatSession, error) {
	return c.ensureSessionInternal(false)
}

func (c *ChatManager) ensureSessionForHello() (*ChatSession, error) {
	return c.ensureSessionInternal(true)
}

func (c *ChatManager) ensureSessionInternal(allowFreshHello bool) (*ChatSession, error) {
	for {
		c.sessionMu.Lock()
		if c.session != nil {
			session := c.session
			c.sessionMu.Unlock()
			if session.IsClosing() {
				return nil, fmt.Errorf("ChatSession 正在关闭，稍后再试")
			}
			return session, nil
		}
		if c.startingSession != nil {
			waitCh := c.startingSessionDone
			c.sessionMu.Unlock()
			if waitCh == nil {
				return nil, fmt.Errorf("ChatSession 正在启动，稍后再试")
			}
			<-waitCh
			continue
		}
		if !c.helloInited {
			c.sessionMu.Unlock()
			return nil, fmt.Errorf("hello尚未初始化，无法创建ChatSession")
		}
		if c.needFreshHello && !allowFreshHello {
			c.sessionMu.Unlock()
			return nil, fmt.Errorf("ChatSession 已退出，请先重新发送hello")
		}

		session := NewChatSession(
			c.clientState,
			c.serverTransport,
			c.hookHub,
			c.transformRegistry,
			WithChatSessionCloseHandler(c.handleSessionClosed),
		)
		c.startingSession = session
		c.startingSessionDone = make(chan struct{})
		c.sessionMu.Unlock()

		err := session.Start(c.ctx)
		if err != nil {
			session.CloseWithReason(chatSessionCloseReasonFatalError)
		}
		c.finishSessionStart(session, allowFreshHello, err)
		if err != nil {
			return nil, err
		}
		if session.IsClosing() {
			return nil, fmt.Errorf("ChatSession 正在关闭，稍后再试")
		}
		return session, nil
	}
}

func (c *ChatManager) requiresFreshHello() bool {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.needFreshHello
}

func (c *ChatManager) setNeedFreshHello(required bool) {
	c.sessionMu.Lock()
	c.needFreshHello = required
	c.sessionMu.Unlock()
}

func (c *ChatManager) needsMqttRebootstrap() bool {
	if c == nil {
		return false
	}
	return c.mqttRebootstrapPending.Load()
}

func (c *ChatManager) clearMqttRebootstrapPending(reason string) {
	if c == nil {
		return
	}
	if c.mqttRebootstrapPending.Swap(false) {
		log.Debugf("设备 %s 清除 MQTT 会话重建标记: reason=%s", c.DeviceID, reason)
	}
}

func (c *ChatManager) hasPendingSpeakRequest() bool {
	if c == nil {
		return false
	}
	c.speakRequestMu.Lock()
	defer c.speakRequestMu.Unlock()
	return c.pendingSpeakRequest != nil
}

func (c *ChatManager) resetSpeakPathAfterMqttRebootstrap(reason string) {
	if c == nil {
		return
	}
	c.lastSpeakPathWarmAt.Store(0)

	c.speakRequestMu.Lock()
	pending := c.pendingSpeakRequest
	c.speakRequestMu.Unlock()
	if pending != nil {
		c.finishPendingSpeakRequest(pending, fmt.Errorf("MQTT链路重建导致主动播报链路已重置: %s", reason))
	}
}

func (c *ChatManager) markMqttConversationStateStale(reason string) {
	if c == nil || c.serverTransport == nil || c.serverTransport.GetTransportType() != types_conn.TransportTypeMqttUdp {
		return
	}
	if c.managerClosing.Load() {
		return
	}
	if reason == "duplicate_hello" && c.hasPendingSpeakRequest() {
		log.Infof("设备 %s 收到 duplicate_hello，检测到待完成 speak_request，按主动播报握手重协商处理，跳过会话重建归一化", c.DeviceID)
		return
	}

	alreadyPending := c.mqttRebootstrapPending.Swap(true)
	if alreadyPending {
		log.Debugf("设备 %s MQTT 会话重建标记已存在，跳过重复归一化: reason=%s", c.DeviceID, reason)
		return
	}

	session := c.GetSession()
	if session != nil && !session.IsClosing() {
		log.Infof("设备 %s MQTT 链路重建，重置当前会话状态: reason=%s", c.DeviceID, reason)
		session.ResetToSilentState()
		c.scheduleRetainedSessionCleanup(session, "mqtt_transport_rebootstrap")
	} else if c.clientState != nil {
		log.Infof("设备 %s MQTT 链路重建，清理残留客户端状态: reason=%s", c.DeviceID, reason)
		c.clientState.Destroy()
		c.clientState.Abort = false
		c.clientState.IsWelcomeSpeaking = false
		c.clientState.IsWelcomePlaying = false
	}

	c.resetSpeakPathAfterMqttRebootstrap(reason)
}

func (c *ChatManager) resetMcpRuntimeOnMqttTransportReady() {
	if c == nil {
		return
	}

	c.helloMu.Lock()
	defer c.helloMu.Unlock()

	c.mcpInitState = chatMcpInitStateIdle
	if c.clientState == nil || c.mcpTransport == nil {
		return
	}

	log.Infof("设备 %s 收到 MQTT online 广播，销毁现有 IoT MCP runtime，准备重新初始化", c.DeviceID)
	closeDeviceMcpRuntime(c.clientState.DeviceID, c.mcpTransport)
}

func (c *ChatManager) HandleMqttTransportReady() {
	c.markMqttConversationStateStale("transport_ready")
	c.resetMcpRuntimeOnMqttTransportReady()
}

func (c *ChatManager) resetSpeakPathAfterGoodbye() {
	c.resetSpeakPathAfterSessionReset(fmt.Errorf("设备端goodbye导致主动播报链路已重置"))
}

func (c *ChatManager) resetSpeakPathAfterServerSessionClose(reason string) {
	c.resetSpeakPathAfterSessionReset(fmt.Errorf("服务端关闭会话导致主动播报链路已重置: %s", reason))
}

func (c *ChatManager) resetSpeakPathAfterSessionReset(err error) {
	if c == nil {
		return
	}
	c.lastSpeakPathWarmAt.Store(0)

	c.speakRequestMu.Lock()
	pending := c.pendingSpeakRequest
	c.speakRequestMu.Unlock()
	if pending != nil {
		c.finishPendingSpeakRequest(pending, err)
	}
}

func (c *ChatManager) getRetainedSessionIdleTimeout() time.Duration {
	if c != nil && c.retainedSessionIdleTimeout > 0 {
		return c.retainedSessionIdleTimeout
	}
	if !viper.IsSet("chat.retained_session_idle_timeout_ms") {
		return defaultRetainedSessionIdleTTL
	}
	ms := viper.GetInt64("chat.retained_session_idle_timeout_ms")
	if ms <= 0 {
		return defaultRetainedSessionIdleTTL
	}
	return time.Duration(ms) * time.Millisecond
}

func (c *ChatManager) cancelRetainedSessionCleanup(reason string) {
	if c == nil {
		return
	}
	c.retainedSessionCleanupMu.Lock()
	timer := c.retainedSessionCleanupTimer
	c.retainedSessionCleanupTimer = nil
	c.retainedSessionCleanupTarget = nil
	c.retainedSessionCleanupMu.Unlock()

	if timer != nil {
		timer.Stop()
		log.Debugf("设备 %s 取消 ChatSession 保留期清理: reason=%s", c.DeviceID, reason)
	}
}

func (c *ChatManager) scheduleRetainedSessionCleanup(session *ChatSession, reason string) {
	if c == nil || session == nil || session.IsClosing() {
		return
	}

	timeout := c.getRetainedSessionIdleTimeout()
	c.cancelRetainedSessionCleanup("reschedule")

	c.retainedSessionCleanupMu.Lock()
	c.retainedSessionCleanupTarget = session
	c.retainedSessionCleanupTimer = time.AfterFunc(timeout, func() {
		c.runRetainedSessionCleanup(session, timeout)
	})
	c.retainedSessionCleanupMu.Unlock()

	log.Infof(
		"设备 %s ChatSession 已进入保留态: timeout=%s reason=%s",
		c.DeviceID,
		timeout,
		reason,
	)
}

func (c *ChatManager) runRetainedSessionCleanup(session *ChatSession, timeout time.Duration) {
	if c == nil || session == nil {
		return
	}

	c.retainedSessionCleanupMu.Lock()
	if c.retainedSessionCleanupTarget != session {
		c.retainedSessionCleanupMu.Unlock()
		return
	}
	c.retainedSessionCleanupTimer = nil
	c.retainedSessionCleanupTarget = nil
	c.retainedSessionCleanupMu.Unlock()

	c.sessionMu.RLock()
	currentSession := c.session
	c.sessionMu.RUnlock()
	if currentSession != session || session.IsClosing() {
		return
	}

	log.Infof("设备 %s ChatSession 空闲超过 %s，执行彻底清理", c.DeviceID, timeout)
	session.CloseWithReason(chatSessionCloseReasonRetainedIdleTimeout)
}

func (c *ChatManager) finishSessionStart(session *ChatSession, allowFreshHello bool, startErr error) {
	var waitCh chan struct{}

	c.sessionMu.Lock()
	if c.startingSession == session {
		waitCh = c.startingSessionDone
		c.startingSession = nil
		c.startingSessionDone = nil
		if startErr == nil && !session.IsClosing() {
			c.session = session
			if allowFreshHello {
				c.needFreshHello = false
			}
		}
	}
	c.sessionMu.Unlock()

	if waitCh != nil {
		close(waitCh)
	}
}

func (c *ChatManager) handleSessionClosed(session *ChatSession, reason string) {
	var waitCh chan struct{}

	c.cancelRetainedSessionCleanup("session_closed")

	c.sessionMu.Lock()
	switch {
	case c.session == session:
		c.session = nil
	case c.startingSession == session:
		waitCh = c.startingSessionDone
		c.startingSession = nil
		c.startingSessionDone = nil
	default:
		c.sessionMu.Unlock()
		log.Debugf("设备 %s 收到过期 ChatSession close 回调，忽略后续清理", c.DeviceID)
		return
	}
	c.sessionMu.Unlock()

	if waitCh != nil {
		close(waitCh)
		log.Debugf("设备 %s ChatSession 在启动阶段关闭，已清理启动状态", c.DeviceID)
		return
	}

	if reason == chatSessionCloseReasonManagerShutdown {
		// manager_shutdown 既可能来自服务端主动 shutdown(true)，也可能来自底层链路
		// 已断开后的资源清理 shutdown(false)。主动 shutdown(true) 场景仍由
		// ServerTransport.Close() 发送 goodbye，避免在被动断连路径误发。
		return
	}

	if c.serverTransport == nil {
		return
	}

	switch c.serverTransport.GetTransportType() {
	case types_conn.TransportTypeWebsocket:
		if err := c.shutdown(true); err != nil {
			log.Warnf("关闭 websocket transport 失败: %v", err)
		}
	case types_conn.TransportTypeMqttUdp:
		if reason == chatSessionCloseReasonRetainedIdleTimeout {
			c.setNeedFreshHello(true)
			c.resetSpeakPathAfterServerSessionClose(reason)
			return
		}
		if !shouldSendMqttGoodbyeOnSessionClose(reason) {
			return
		}
		c.setNeedFreshHello(true)
		c.resetSpeakPathAfterServerSessionClose(reason)
		if err := c.serverTransport.SendMqttGoodbye(); err != nil {
			log.Warnf("发送 mqtt goodbye 失败: %v", err)
		}
	}
}

func shouldSendMqttGoodbyeOnSessionClose(reason string) bool {
	switch reason {
	case chatSessionCloseReasonExplicitExit,
		chatSessionCloseReasonFatalError,
		chatSessionCloseReasonAudioIdleTimeout:
		return true
	default:
		return false
	}
}

func (c *ChatManager) shutdown(closeTransport bool) error {
	var shutdownErr error

	c.closeOnce.Do(func() {
		c.cancelRetainedSessionCleanup("manager_shutdown")

		if c.clientState != nil {
			log.Infof("关闭 ChatManager, 设备 %s", c.clientState.DeviceID)
		}

		c.sessionMu.RLock()
		session := c.session
		startingSession := c.startingSession
		c.sessionMu.RUnlock()

		if session != nil {
			session.CloseWithReason(chatSessionCloseReasonManagerShutdown)
		}
		if startingSession != nil && startingSession != session {
			startingSession.CloseWithReason(chatSessionCloseReasonManagerShutdown)
		}

		if c.clientState != nil && c.mcpTransport != nil {
			mcp.CloseDeviceIotOverMcp(c.clientState.DeviceID, c.mcpTransport)
		}

		if c.hookHub != nil {
			c.hookHub.Close()
		}

		if closeTransport {
			c.managerClosing.Store(true)
			defer c.managerClosing.Store(false)

			if c.serverTransport != nil {
				shutdownErr = c.serverTransport.Close()
			} else if c.transport != nil {
				shutdownErr = c.transport.Close()
			}
		} else if c.serverTransport != nil {
			if err := c.serverTransport.CloseWithoutTransport(); err != nil {
				log.Warnf("关闭 server transport 包装层失败: %v", err)
			}
		}

		if c.cancel != nil {
			c.cancel()
		}
	})

	return shutdownErr
}

func (c *ChatManager) Close() error {
	return c.shutdown(true)
}

func (c *ChatManager) OnClose(deviceId string) {
	log.Infof("设备 %s 断开连接", deviceId)
	if c.managerClosing.Load() {
		return
	}
	if err := c.shutdown(false); err != nil {
		log.Warnf("连接关闭后的资源清理失败: %v", err)
	}
}

func (c *ChatManager) GetClientState() *ClientState {
	return c.clientState
}

func (c *ChatManager) GetDeviceId() string {
	return c.clientState.DeviceID
}

func (c *ChatManager) GetTransportType() string {
	if c == nil || c.serverTransport == nil {
		return ""
	}
	if c.ctx != nil && c.ctx.Err() != nil {
		return ""
	}
	if c.serverTransport.IsClosed() {
		return ""
	}
	if awareTransport, ok := c.transport.(brokerOnlineAwareTransport); ok && !awareTransport.IsBrokerOnline() {
		return ""
	}
	return c.serverTransport.GetTransportType()
}

func (c *ChatManager) WarmupMcp() {
	c.helloMu.Lock()
	defer c.helloMu.Unlock()
	c.scheduleMcpInitLocked()
}

func (c *ChatManager) GetSession() *ChatSession {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.session
}

func (c *ChatManager) GetServerTransport() *ServerTransport {
	if c == nil {
		return nil
	}
	return c.serverTransport
}

func (c *ChatManager) SendNotify(audioURL string, subtitles []msg.NotifySubtitle) error {
	if c == nil || c.serverTransport == nil {
		return fmt.Errorf("chat manager or server transport is nil")
	}
	return c.serverTransport.SendNotify(audioURL, subtitles)
}

func (c *ChatManager) InjectMessage(message string, skipLlm bool, autoListen bool) error {
	c.cancelRetainedSessionCleanup("inject_message")
	if err := c.prepareSpeakPathForInjectedSpeech(message, autoListen); err != nil {
		return err
	}
	session, err := c.ensureSession()
	if err != nil {
		return err
	}
	options := llmResponseChannelOptions{
		onTTSPlaybackStart: c.newInjectedSpeechStartHook(),
		ttsTurnEndPolicy:   injectedSpeechTTSTurnEndPolicy(autoListen),
	}
	if skipLlm {
		return session.AddTextToTTSQueueWithOptions(message, options)
	}
	return session.AddAsrResultToQueueWithOptions(message, nil, options)
}

func (c *ChatManager) prepareSpeakPathForInjectedSpeech(previewText string, autoListen bool) error {
	if c == nil || c.serverTransport == nil {
		return nil
	}
	if c.serverTransport.GetTransportType() != types_conn.TransportTypeMqttUdp {
		log.Debugf("设备 %s 注入消息跳过 speak_request: transport=%s", c.DeviceID, c.serverTransport.GetTransportType())
		return nil
	}
	if !c.shouldSendSpeakRequest(time.Now()) {
		log.Debugf("设备 %s 注入消息复用现有播报链路，跳过 speak_request", c.DeviceID)
		return nil
	}
	if _, err := c.ensureClientSessionID(); err != nil {
		return err
	}

	needSessionBootstrap := c.requiresHelloBootstrapForSession()
	pending, created := c.getOrCreatePendingSpeakRequest()
	if created {
		if err := c.serverTransport.SendSpeakRequest(previewText, autoListen); err != nil {
			c.finishPendingSpeakRequest(pending, err)
			return err
		}
		log.Infof("设备 %s 已发送 speak_request，session_id=%s", c.DeviceID, pending.sessionID)
	}

	waitCtx := c.ctx
	if waitCtx == nil {
		waitCtx = context.Background()
	}
	if err := c.waitPendingSpeakRequest(waitCtx, pending); err != nil {
		return err
	}
	if needSessionBootstrap {
		if err := c.waitForInjectedSpeechSession(waitCtx); err != nil {
			return err
		}
	}
	return nil
}

func (c *ChatManager) shouldSendSpeakRequest(now time.Time) bool {
	if c == nil || c.serverTransport == nil {
		return false
	}
	if c.serverTransport.GetTransportType() != types_conn.TransportTypeMqttUdp {
		log.Debugf("设备 %s speak_request 判定: transport=%s，无需发送", c.DeviceID, c.serverTransport.GetTransportType())
		return false
	}
	if c.requiresHelloBootstrapForSession() {
		log.Debugf("设备 %s speak_request 判定: ChatSession 依赖新 hello 建立，需发送", c.DeviceID)
		return true
	}
	if c.needsMqttRebootstrap() {
		log.Debugf("设备 %s speak_request 判定: MQTT 链路待重建，需发送", c.DeviceID)
		return true
	}
	if c.isConversationActive() {
		log.Debugf("设备 %s speak_request 判定: 当前处于会话中，跳过发送", c.DeviceID)
		return false
	}

	warmAt := c.currentSpeakPathWarmAt()
	if warmAt <= 0 {
		log.Debugf("设备 %s speak_request 判定: 无可复用热链路，需发送", c.DeviceID)
		return true
	}
	reuseWindow := speakRequestReuseWindow()
	idleFor := now.Sub(time.UnixMilli(warmAt))
	if idleFor <= reuseWindow {
		log.Debugf("设备 %s speak_request 判定: 热链路仍有效 idle_for=%s reuse_window=%s，跳过发送", c.DeviceID, idleFor, reuseWindow)
		return false
	}
	log.Debugf("设备 %s speak_request 判定: 热链路已过期 idle_for=%s reuse_window=%s，需发送", c.DeviceID, idleFor, reuseWindow)
	return true
}

func (c *ChatManager) requiresHelloBootstrapForSession() bool {
	if c == nil {
		return false
	}
	if c.GetSession() != nil {
		return false
	}
	if !c.helloInited {
		return true
	}
	return c.requiresFreshHello()
}

func (c *ChatManager) ensureClientSessionID() (string, error) {
	if c == nil || c.clientState == nil {
		return "", fmt.Errorf("clientState 未初始化")
	}
	sessionID := strings.TrimSpace(c.clientState.SessionID)
	if sessionID != "" {
		return sessionID, nil
	}
	session, err := auth.A().CreateSession(c.DeviceID)
	if err != nil {
		return "", fmt.Errorf("创建会话失败: %v", err)
	}
	c.clientState.SessionID = session.ID
	return session.ID, nil
}

func (c *ChatManager) waitForInjectedSpeechSession(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if _, err := c.ensureSession(); err == nil {
		return nil
	} else if !shouldRetryInjectedSpeechSessionWait(err) {
		return err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.speakReadyTimeout
	if timeout <= 0 {
		timeout = defaultSpeakReadyTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		if _, err := c.ensureSession(); err == nil {
			return nil
		} else {
			lastErr = err
			if !shouldRetryInjectedSpeechSessionWait(err) {
				return err
			}
		}

		select {
		case <-waitCtx.Done():
			if waitCtx.Err() == context.DeadlineExceeded {
				if lastErr != nil {
					return fmt.Errorf("等待 ChatSession 建立超时: %w", lastErr)
				}
				return fmt.Errorf("等待 ChatSession 建立超时")
			}
			return waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func shouldRetryInjectedSpeechSessionWait(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "hello尚未初始化") ||
		strings.Contains(msg, "重新发送hello") ||
		strings.Contains(msg, "正在启动") ||
		strings.Contains(msg, "正在关闭")
}

func (c *ChatManager) isConversationActive() bool {
	if c == nil || c.clientState == nil {
		return false
	}
	if phase := c.clientState.GetListenPhase(); phase != "" && phase != ListenPhaseIdle {
		return true
	}
	switch c.clientState.GetStatus() {
	case ClientStatusListening, ClientStatusLLMStart, ClientStatusTTSStart:
		return true
	}
	session := c.GetSession()
	return session != nil && session.IsTTSActive()
}

func (c *ChatManager) getOrCreatePendingSpeakRequest() (*pendingSpeakRequest, bool) {
	c.speakRequestMu.Lock()
	defer c.speakRequestMu.Unlock()

	if c.pendingSpeakRequest != nil {
		return c.pendingSpeakRequest, false
	}

	sessionID := ""
	if c.clientState != nil {
		sessionID = strings.TrimSpace(c.clientState.SessionID)
	}
	pending := &pendingSpeakRequest{
		sessionID: sessionID,
		done:      make(chan struct{}),
	}
	timeout := c.speakReadyTimeout
	if timeout <= 0 {
		timeout = defaultSpeakReadyTimeout
	}
	pending.timer = time.AfterFunc(timeout, func() {
		c.finishPendingSpeakRequest(pending, fmt.Errorf("等待 speak_ready 超时"))
	})
	c.pendingSpeakRequest = pending
	return pending, true
}

func (c *ChatManager) waitPendingSpeakRequest(ctx context.Context, pending *pendingSpeakRequest) error {
	if pending == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-pending.done:
		return pending.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *ChatManager) finishPendingSpeakRequest(pending *pendingSpeakRequest, err error) {
	if pending == nil {
		return
	}
	c.speakRequestMu.Lock()
	if c.pendingSpeakRequest == pending {
		c.pendingSpeakRequest = nil
	}
	c.speakRequestMu.Unlock()
	pending.resolve(err)
}

func (c *ChatManager) refreshSpeakPathWarmFromTransport() {
	if c == nil || c.serverTransport == nil || !c.serverTransport.HasActiveUDPBinding() {
		return
	}
	if c.hasPendingSpeakRequest() {
		log.Debugf("设备 %s 当前存在待完成 speak_request，跳过刷新热链路", c.DeviceID)
		return
	}
	if c.needsMqttRebootstrap() {
		log.Debugf("设备 %s 当前存在 MQTT 会话重建标记，跳过刷新热链路", c.DeviceID)
		return
	}
	if ts := c.serverTransport.GetUDPLastActiveTs(); ts > 0 {
		c.updateSpeakPathWarmAt(ts)
		return
	}
	c.markSpeakPathWarm(time.Now())
}

func (c *ChatManager) currentSpeakPathWarmAt() int64 {
	if c == nil {
		return 0
	}
	latest := c.lastSpeakPathWarmAt.Load()
	if c.serverTransport != nil {
		if transportTs := c.serverTransport.GetUDPLastActiveTs(); transportTs > latest {
			latest = transportTs
		}
	}
	return latest
}

func (c *ChatManager) markSpeakPathWarm(ts time.Time) {
	if ts.IsZero() {
		ts = time.Now()
	}
	c.updateSpeakPathWarmAt(ts.UnixMilli())
}

func (c *ChatManager) updateSpeakPathWarmAt(ts int64) {
	if c == nil || ts <= 0 {
		return
	}
	for {
		current := c.lastSpeakPathWarmAt.Load()
		if current >= ts {
			return
		}
		if c.lastSpeakPathWarmAt.CompareAndSwap(current, ts) {
			return
		}
	}
}

func speakRequestReuseWindow() time.Duration {
	if !viper.IsSet("chat.speak_request_reuse_window_ms") {
		return defaultSpeakRequestReuseWindow
	}
	ms := viper.GetInt64("chat.speak_request_reuse_window_ms")
	if ms <= 0 {
		return defaultSpeakRequestReuseWindow
	}
	return time.Duration(ms) * time.Millisecond
}

func (c *ChatManager) newInjectedSpeechStartHook() func() {
	if c == nil {
		return nil
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			if c.serverTransport == nil || c.serverTransport.GetTransportType() != types_conn.TransportTypeMqttUdp {
				return
			}
			c.markSpeakPathWarm(time.Now())
		})
	}
}

func injectedSpeechTTSTurnEndPolicy(autoListen bool) ttsTurnEndPolicy {
	if autoListen {
		return ttsTurnEndPolicyNone
	}
	return ttsTurnEndPolicyGoodbyeAndIdle
}

func (c *ChatManager) handleTTSTurnEndPolicy(ctx context.Context, policy ttsTurnEndPolicy, stopErr error) {
	if c == nil || policy == ttsTurnEndPolicyNone {
		return
	}
	if stopErr != nil {
		log.Debugf("设备 %s TTS turn end policy skipped: policy=%d err=%v", c.DeviceID, policy, stopErr)
		return
	}

	switch policy {
	case ttsTurnEndPolicyGoodbyeAndIdle:
		if c.serverTransport == nil || c.serverTransport.GetTransportType() != types_conn.TransportTypeMqttUdp {
			return
		}
		if !ttsTurnPlaybackSettledFromContext(ctx) {
			timer := time.NewTimer(ttsPlaybackCompletionGrace)
			defer stopTimer(timer)

			select {
			case <-timer.C:
			case <-c.ctx.Done():
				log.Debugf("设备 %s TTS turn end policy delayed goodbye canceled: %v", c.DeviceID, c.ctx.Err())
				return
			}
		}
		if c.managerClosing.Load() || c.serverTransport == nil || c.serverTransport.GetTransportType() != types_conn.TransportTypeMqttUdp {
			return
		}

		session := c.GetSession()
		if session == nil || session.IsClosing() {
			log.Debugf("设备 %s TTS turn end policy skipped: session already closed", c.DeviceID)
			return
		}
		session.CloseWithReason(chatSessionCloseReasonExplicitExit)
	}
}

func (c *ChatManager) InjectOpenClawResponse(event openclaw.ResponseDelivery) error {
	c.cancelRetainedSessionCleanup("openclaw_response")
	session, err := c.ensureSession()
	if err != nil {
		return err
	}
	return session.InjectOpenClawResponse(event)
}

func (c *ChatManager) ExitChat() error {
	session := c.GetSession()
	if session == nil {
		return nil
	}
	session.DoExitChat()
	return nil
}

func (c *ChatManager) resetOpenClawModeOnHello(agentIDs ...string) {
	deviceID := strings.TrimSpace(c.clientState.DeviceID)
	if deviceID == "" {
		return
	}

	openclawManager := openclaw.GetManager()
	seen := make(map[string]struct{}, len(agentIDs))
	for _, agentID := range agentIDs {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		if _, exists := seen[agentID]; exists {
			continue
		}
		seen[agentID] = struct{}{}
		if openclawManager.ExitMode(agentID, deviceID) {
			log.Infof("设备 %s 在 hello 后重置OpenClaw模式: agent=%s", deviceID, agentID)
		}
	}
}

func (c *ChatManager) refreshDeviceConfigOnHello() error {
	configProvider, err := userconfig.GetProvider(viper.GetString("config_provider.type"))
	if err != nil {
		return fmt.Errorf("获取配置提供者失败: %w", err)
	}

	deviceConfig, err := configProvider.GetUserConfig(c.clientState.Ctx, c.clientState.DeviceID)
	if err != nil {
		return fmt.Errorf("获取设备配置失败: %w", err)
	}
	deviceConfig.MemoryMode = NormalizeMemoryMode(deviceConfig.MemoryMode)
	deviceConfig.SpeakerChatMode = NormalizeSpeakerChatMode(deviceConfig.SpeakerChatMode)

	prevAgentID := c.clientState.AgentID
	c.clientState.AgentID = deviceConfig.AgentId
	c.clientState.DeviceConfig = deviceConfig
	c.clientState.SystemPrompt = deviceConfig.SystemPrompt
	c.clientState.SpeakerTTSConfig = nil
	applyOutputAudioFormatForTTS(c.clientState)

	log.Infof("设备 %s hello 刷新配置成功，agent: %s -> %s", c.clientState.DeviceID, prevAgentID, deviceConfig.AgentId)
	return nil
}
