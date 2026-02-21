package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	"xiaozhi-esp32-server-golang/internal/app/server/auth"
	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	"xiaozhi-esp32-server-golang/internal/data/history"
	. "xiaozhi-esp32-server-golang/internal/data/msg"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/memory"
	"xiaozhi-esp32-server-golang/internal/domain/memory/llm_memory"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

type AsrResponseChannelItem struct {
	ctx           context.Context
	text          string
	speakerResult *speaker.IdentifyResult
}

type ChatSession struct {
	clientState     *ClientState
	asrManager      *ASRManager
	ttsManager      *TTSManager
	llmManager      *LLMManager
	speakerManager  *SpeakerManager
	serverTransport *ServerTransport

	ctx    context.Context
	cancel context.CancelFunc

	chatTextQueue *util.Queue[AsrResponseChannelItem]

	// 声纹识别结果暂存（带锁保护）
	speakerResultMu      sync.RWMutex
	pendingSpeakerResult *speaker.IdentifyResult
	speakerResultReady   chan struct{} // 仅用于通知就绪，不传数据

	// Close 保护，防止多次关闭
	closeOnce sync.Once
	closed    bool
}

type ChatSessionOption func(*ChatSession)

func NewChatSession(clientState *ClientState, serverTransport *ServerTransport, opts ...ChatSessionOption) *ChatSession {
	s := &ChatSession{
		clientState:        clientState,
		serverTransport:    serverTransport,
		chatTextQueue:      util.NewQueue[AsrResponseChannelItem](10),
		speakerResultReady: make(chan struct{}, 1), // 缓冲为1，避免阻塞
	}
	for _, opt := range opts {
		opt(s)
	}

	s.asrManager = NewASRManager(clientState, serverTransport)
	s.asrManager.session = s // 设置 session 引用
	s.ttsManager = NewTTSManager(clientState, serverTransport)
	s.llmManager = NewLLMManager(clientState, serverTransport, s.ttsManager)

	// 如果启用声纹识别，创建声纹管理器
	if clientState.IsSpeakerEnabled() {
		// 声纹识别运行时配置
		baseURL := strings.TrimSpace(viper.GetString("voice_identify.base_url"))
		serviceMode := strings.ToLower(strings.TrimSpace(viper.GetString("voice_identify.mode")))

		speakerConfig := map[string]interface{}{}
		if baseURL != "" {
			speakerConfig["base_url"] = baseURL
		}
		if serviceMode != "" {
			speakerConfig["mode"] = serviceMode
		}
		if viper.IsSet("voice_identify.threshold") {
			speakerConfig["threshold"] = viper.GetFloat64("voice_identify.threshold")
		}

		if serviceMode == "embed" || baseURL != "" {
			provider, err := speaker.GetSpeakerProvider(speakerConfig)
			if err != nil {
				log.Warnf("创建声纹识别提供者失败: %v", err)
			} else {
				clientState.SpeakerProvider = provider
				s.speakerManager = NewSpeakerManager(provider)
				log.Debugf("设备 %s 启用声纹识别", clientState.DeviceID)

				// 设置异步获取声纹结果的回调
				clientState.OnVoiceSilenceSpeakerCallback = func(ctx context.Context) {
					log.Debugf("[声纹识别] OnVoiceSilenceSpeakerCallback 被调用, deviceID: %s", clientState.DeviceID)

					// 异步获取声纹结果
					go func() {
						log.Debugf("[声纹识别] 开始异步获取声纹识别结果, deviceID: %s", clientState.DeviceID)

						// 检查 speakerManager 是否激活
						if !s.speakerManager.IsActive() {
							//log.Warnf("[声纹识别] speakerManager 未激活，无法获取识别结果")
							return
						}
						// 清空之前的结果
						s.speakerResultMu.Lock()
						oldResult := s.pendingSpeakerResult
						s.pendingSpeakerResult = nil
						s.speakerResultMu.Unlock()
						if oldResult != nil {
							log.Debugf("[声纹识别] 清空之前的识别结果: identified=%v, speaker_id=%s", oldResult.Identified, oldResult.SpeakerID)
						}

						// 清空就绪通知（非阻塞）
						select {
						case <-s.speakerResultReady:
							log.Debugf("[声纹识别] 清空就绪通知通道")
						default:
							log.Debugf("[声纹识别] 就绪通知通道已为空")
						}

						result, err := s.speakerManager.FinishAndIdentify(ctx)
						if err != nil {
							log.Warnf("[声纹识别] 获取声纹识别结果失败: %v, deviceID: %s", err, clientState.DeviceID)
							// 声纹识别失败不影响主流程，存储 nil 结果
							s.speakerResultMu.Lock()
							s.pendingSpeakerResult = nil
							s.speakerResultMu.Unlock()
							log.Debugf("[声纹识别] 已存储 nil 结果（识别失败）")
						} else if result != nil && result.Identified {
							log.Infof("[声纹识别] 识别到说话人: %s (置信度: %.4f, 阈值: %.4f), deviceID: %s",
								result.SpeakerName, result.Confidence, result.Threshold, clientState.DeviceID)
							log.Debugf("[声纹识别] 识别结果详情: speaker_id=%s, speaker_name=%s, confidence=%.4f, threshold=%.4f",
								result.SpeakerID, result.SpeakerName, result.Confidence, result.Threshold)
							s.speakerResultMu.Lock()
							s.pendingSpeakerResult = result
							s.speakerResultMu.Unlock()
							log.Debugf("[声纹识别] 已存储识别结果（已识别）")
						} else {
							// 未识别到说话人，也存储结果
							if result != nil {
								log.Debugf("[声纹识别] 未识别到说话人: identified=%v, confidence=%.4f, threshold=%.4f, deviceID: %s",
									result.Identified, result.Confidence, result.Threshold, clientState.DeviceID)
							} else {
								log.Debugf("[声纹识别] 识别结果为 nil, deviceID: %s", clientState.DeviceID)
							}
							s.speakerResultMu.Lock()
							s.pendingSpeakerResult = result
							s.speakerResultMu.Unlock()
							log.Debugf("[声纹识别] 已存储识别结果（未识别）")
						}

						// 通知结果就绪
						select {
						case s.speakerResultReady <- struct{}{}:
							log.Debugf("[声纹识别] 已发送结果就绪通知, deviceID: %s", clientState.DeviceID)
						default:
							log.Warnf("[声纹识别] 结果就绪通知通道已满，无法发送通知, deviceID: %s", clientState.DeviceID)
						}
					}()
				}
			}
		}
	}

	// 设置 ASR 首次返回字符的回调
	clientState.OnAsrFirstTextCallback = func(text string, isFinal bool) {
		log.Debugf("ASR首次返回字符: device=%s, text=%s, isFinal=%v", clientState.DeviceID, text, isFinal)
		if clientState.IsRealTime() && viper.GetInt("chat.realtime_mode") == 4 {
			clientState.AfterAsrSessionCtx.Cancel()
			s.InterruptAndClearTTSQueue()
		}
	}

	return s
}

func (s *ChatSession) Start(pctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(pctx)

	err := s.InitAsrLlmTts()
	if err != nil {
		log.Errorf("初始化ASR/LLM/TTS失败: %v", err)
		return err
	}

	// 异步加载历史消息，不阻塞会话启动
	go func() {
		err := s.initHistoryMessages()
		if err != nil {
			log.Errorf("初始化对话历史失败: %v", err)
		}
	}()

	go s.CmdMessageLoop(s.ctx)   //处理信令消息
	go s.AudioMessageLoop(s.ctx) //处理音频数据
	go s.processChatText(s.ctx)  //处理 asr后 的对话消息
	go s.llmManager.Start(s.ctx) //处理 llm后 的一系列返回消息
	go s.ttsManager.Start(s.ctx) //处理 tts的 消息队列

	return nil
}

// 初始化历史对话记录到内存中
func (s *ChatSession) initHistoryMessages() error {
	var historyMessages []*schema.Message
	var err error

	if s.clientState.GetMemoryMode() == MemoryModeNone {
		log.Debugf("设备 %s 记忆模式=none，跳过历史消息加载", s.clientState.DeviceID)
		return nil
	}

	// 根据配置选择数据源（无优先级关系，直接选择）
	useRedis := s.shouldUseRedis()
	useManager := s.shouldUseManager()

	// 验证必要字段：DeviceID 不能为空
	if s.clientState.DeviceID == "" {
		log.Debugf("DeviceID 为空，跳过历史消息加载（可能在 hello 消息之前调用）")
		return nil
	}

	// 根据配置选择数据源（无优先级关系，直接选择）
	if useRedis {
		// 从 Redis 加载
		historyMessages, err = llm_memory.Get().GetMessages(
			s.ctx,
			s.clientState.DeviceID,
			s.clientState.AgentID,
			20)
		if err != nil {
			log.Warnf("从 Redis 加载历史消息失败: %v", err)
			return err
		}
		log.Infof("从 Redis 加载了 %d 条历史消息", len(historyMessages))
	} else if useManager {
		// 从 Manager 加载
		historyMessages, err = s.loadFromManager()
		if err != nil {
			log.Warnf("从 Manager 加载历史消息失败: %v", err)
			return err
		}
		log.Infof("从 Manager 加载了 %d 条历史消息", len(historyMessages))
	} else {
		// 两个数据源都未配置，不加载历史消息
		log.Debugf("Redis 和 Manager 都未配置，跳过历史消息加载")
		return nil
	}

	if len(historyMessages) > 0 {
		s.clientState.InitMessages(historyMessages)
		log.Infof("成功加载 %d 条历史消息", len(historyMessages))
	} else {
		log.Debugf("未加载到历史消息（可能没有历史记录）")
	}

	return nil
}

// shouldUseRedis 判断是否使用 Redis 作为数据源
func (s *ChatSession) shouldUseRedis() bool {
	// 根据 config_provider.type 判断
	providerType := viper.GetString("config_provider.type")
	return providerType == "redis"
}

// shouldUseManager 判断是否使用 Manager 作为数据源
func (s *ChatSession) shouldUseManager() bool {
	// 根据 config_provider.type 判断
	providerType := viper.GetString("config_provider.type")
	return providerType == "manager"
}

// loadFromManager 从 Manager 数据库加载历史消息
func (s *ChatSession) loadFromManager() ([]*schema.Message, error) {
	// 创建 HistoryClient
	historyCfg := history.HistoryClientConfig{
		BaseURL:   util.GetBackendURL(),
		AuthToken: viper.GetString("manager.history_auth_token"),
		Timeout:   viper.GetDuration("manager.history_timeout"),
		Enabled:   true,
	}
	client := history.NewHistoryClient(historyCfg)

	if s.clientState.DeviceID == "" || s.clientState.AgentID == "" {
		return []*schema.Message{}, nil
	}

	req := &history.GetMessagesRequest{
		DeviceID:  s.clientState.DeviceID,
		AgentID:   s.clientState.AgentID,
		SessionID: s.clientState.SessionID,
		Limit:     20,
	}

	resp, err := client.GetMessages(s.ctx, req)
	if err != nil {
		return nil, err
	}

	// 转换为 schema.Message 格式
	messages := make([]*schema.Message, 0, len(resp.Messages))
	for _, item := range resp.Messages {
		var msg *schema.Message
		switch item.Role {
		case "user":
			msg = schema.UserMessage(item.Content)
		case "assistant":
			msg = schema.AssistantMessage(item.Content, item.ToolCalls)
		case "tool":
			msg = schema.ToolMessage(item.Content, item.ToolCallID)
		case "system":
			msg = schema.SystemMessage(item.Content)
		default:
			log.Warnf("未知的消息角色: %s", item.Role)
			continue
		}

		messages = append(messages, msg)
	}

	for _, msg := range messages {
		log.Debugf("历史消息: %+v", msg)
	}

	return messages, nil
}

// 在mqtt 收到type: listen, state: start后进行
func (c *ChatSession) InitAsrLlmTts() error {
	//初始化asr结构
	c.clientState.InitAsr()

	// 初始化memory（memory不在资源池中）
	memoryMode := c.clientState.GetMemoryMode()
	memoryConfig := c.clientState.DeviceConfig.Memory
	memoryType := memory.MemoryType(memoryConfig.Provider)
	if memoryMode != MemoryModeLong {
		memoryType = memory.MemoryTypeNone
	}

	memoryProvider, err := memory.GetProvider(memoryType, memoryConfig.Config)
	if err != nil {
		return fmt.Errorf("创建 Memory 提供者失败: %v", err)
	}
	c.clientState.MemoryProvider = memoryProvider

	if memoryMode == MemoryModeLong {
		// 初始化memory context（仅长记忆模式）
		context, err := memoryProvider.GetContext(c.ctx, c.clientState.GetDeviceIDOrAgentID(), 500)
		if err != nil {
			log.Warnf("初始化memory context失败: %v", err)
		}
		c.clientState.MemoryContext = context
	} else {
		c.clientState.MemoryContext = ""
	}

	return nil
}

func (c *ChatSession) CmdMessageLoop(ctx context.Context) {
	recvFailCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Infof("设备 %s recvCmd context cancel", c.clientState.DeviceID)
			return
		default:
		}

		if recvFailCount > 3 {
			log.Errorf("recv cmd timeout: %v", recvFailCount)
			return
		}

		message, err := c.serverTransport.RecvCmd(ctx, 120)
		if err != nil {
			log.Errorf("recv cmd error: %v", err)
			recvFailCount = recvFailCount + 1
			continue
		}
		if message == nil {
			continue
		}
		recvFailCount = 0
		log.Infof("收到文本消息: %s", string(message))
		if err := c.HandleTextMessage(message); err != nil {
			log.Errorf("处理文本消息失败: %v, 消息内容: %s", err, string(message))
			continue
		}
	}
}

func (c *ChatSession) AudioMessageLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Debugf("设备 %s recvCmd context cancel", c.clientState.DeviceID)
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
		log.Debugf("收到音频数据，大小: %d 字节", len(message))
		isAuth := viper.GetBool("auth.enable")
		if isAuth {
			if !c.clientState.IsActivated {
				log.Debugf("设备 %s 未激活, 跳过音频数据", c.clientState.DeviceID)
				continue
			}
		}
		if c.clientState.GetClientVoiceStop() {
			log.Debug("客户端停止说话, 跳过音频数据")
			continue
		}

		if ok := c.HandleAudioMessage(message); !ok {
			log.Errorf("音频缓冲区已满: %v", err)
		}
	}
}

// handleTextMessage 处理文本消息
func (c *ChatSession) HandleTextMessage(message []byte) error {
	var clientMsg ClientMessage
	if err := json.Unmarshal(message, &clientMsg); err != nil {
		log.Errorf("解析消息失败: %v", err)
		return fmt.Errorf("解析消息失败: %v", err)
	}

	// 处理不同类型的消息
	switch clientMsg.Type {
	case MessageTypeHello:
		return c.HandleHelloMessage(&clientMsg)
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
		// 未知消息类型，直接回显
		return fmt.Errorf("未知消息类型: %s", clientMsg.Type)
	}
}

// HandleAudioMessage 处理音频消息
func (c *ChatSession) HandleAudioMessage(data []byte) bool {
	select {
	case c.clientState.OpusAudioBuffer <- data:
		return true
	default:
		log.Warnf("音频缓冲区已满, 丢弃音频数据")
	}
	return false
}

// handleHelloMessage 处理 hello 消息
func (s *ChatSession) HandleHelloMessage(msg *ClientMessage) error {
	if msg.Transport == types_conn.TransportTypeWebsocket {
		return s.HandleWebsocketHelloMessage(msg)
	} else if msg.Transport == types_conn.TransportTypeMqttUdp {
		return s.HandleMqttHelloMessage(msg)
	}
	return fmt.Errorf("不支持的传输类型: %s", msg.Transport)
}

func (s *ChatSession) HandleMqttHelloMessage(msg *ClientMessage) error {
	s.HandleCommonHelloMessage(msg)

	clientState := s.clientState

	udpExternalHost := viper.GetString("udp.external_host")
	udpExternalPort := viper.GetInt("udp.external_port")

	aesKey, err := s.serverTransport.GetData("aes_key")
	if err != nil {
		return fmt.Errorf("获取aes_key失败: %v", err)
	}
	fullNonce, err := s.serverTransport.GetData("full_nonce")
	if err != nil {
		return fmt.Errorf("获取full_nonce失败: %v", err)
	}

	strAesKey, ok := aesKey.(string)
	if !ok {
		return fmt.Errorf("aes_key不是字符串")
	}
	strFullNonce, ok := fullNonce.(string)
	if !ok {
		return fmt.Errorf("full_nonce不是字符串")
	}

	udpConfig := &UdpConfig{
		Server: udpExternalHost,
		Port:   udpExternalPort,
		Key:    strAesKey,
		Nonce:  strFullNonce,
	}

	// 发送响应
	return s.serverTransport.SendHello("udp", &clientState.OutputAudioFormat, udpConfig)
}

func (s *ChatSession) HandleCommonHelloMessage(msg *ClientMessage) error {
	// 创建新会话
	session, err := auth.A().CreateSession(msg.DeviceID)
	if err != nil {
		return fmt.Errorf("创建会话失败: %v", err)
	}

	// 更新客户端状态
	s.clientState.SessionID = session.ID

	if isMcp, ok := msg.Features["mcp"]; ok && isMcp {
		go initMcp(s.clientState, s.serverTransport)
	}

	clientState := s.clientState

	clientState.InputAudioFormat = *msg.AudioParams

	s.asrManager.ProcessVadAudio(clientState.Ctx, s.Close)

	return nil
}

func (s *ChatSession) HandleWebsocketHelloMessage(msg *ClientMessage) error {
	err := s.HandleCommonHelloMessage(msg)
	if err != nil {
		return err
	}

	return s.serverTransport.SendHello("websocket", &s.clientState.OutputAudioFormat, nil)
}

// handleListenMessage 处理监听消息
func (s *ChatSession) HandleListenMessage(msg *ClientMessage) error {
	// 根据状态处理
	switch msg.State {
	case MessageStateStart:
		s.HandleListenStart(msg)
	case MessageStateStop:
		s.HandleListenStop()
	case MessageStateDetect:
		s.HandleListenDetect(msg)
	}

	// 记录日志
	log.Infof("设备 %s 更新音频监听状态: %s", msg.DeviceID, msg.State)
	return nil
}

func (s *ChatSession) HandleListenDetect(msg *ClientMessage) error {
	if s.clientState.Status == ClientStatusListening {
		log.Debugf("设备 %s 正在监听, 跳过唤醒词检测", msg.DeviceID)
		return nil
	}
	// 唤醒词检测
	s.StopSpeaking(false)

	// 如果有文本，处理唤醒词
	if msg.Text != "" {
		isActivated, err := s.CheckDeviceActivated()
		if err != nil {
			log.Errorf("检查设备激活状态失败: %v", err)
			return err
		}
		if !isActivated {
			return nil
		}

		text := msg.Text
		// 移除标点符号和处理长度
		text = removePunctuation(text)

		// 检查是否是唤醒词
		isWakeupWord := isWakeupWord(text)
		enableGreeting := viper.GetBool("enable_greeting") // 从配置获取

		var needStartChat bool
		if !isWakeupWord || (isWakeupWord && enableGreeting) {
			needStartChat = true
		}
		if needStartChat {
			// 否则开始对话
			if enableGreeting && isWakeupWord {
				//进行tts欢迎语
				if !s.clientState.IsWelcomeSpeaking {
					s.HandleWelcome()
				}
			} else {
				s.clientState.Destroy()
				//进行llm->tts聊天
				if err := s.AddAsrResultToQueue(text, nil); err != nil {
					log.Errorf("开始对话失败: %v", err)
				}
			}
		}
	}
	return nil
}

func (s *ChatSession) HandleNotActivated() {
	configProvider, err := user_config.GetProvider(viper.GetString("config_provider.type"))
	if err != nil {
		log.Errorf("获取配置提供者失败: %v", err)
		return
	}

	code, challenge, message, timeoutMs := configProvider.GetActivationInfo(s.clientState.Ctx, s.clientState.DeviceID, "client_id")
	if code == 0 {
		log.Errorf("获取激活信息失败: %v", err)
		return
	}

	log.Infof("激活码: %d, 挑战码: %s, 消息: %s, 超时时间: %d", code, challenge, message, timeoutMs)

	s.ttsManager.EnqueueTtsStart(s.clientState.Ctx)
	defer s.ttsManager.EnqueueTtsStop(s.clientState.Ctx)

	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	_ = s.ttsManager.handleTextResponse(s.clientState.AfterAsrSessionCtx.Get(sessionCtx), llm_common.LLMResponseStruct{
		Text: fmt.Sprintf("请在后台添加设备，激活码: %d", code),
	}, false)

}

func (s *ChatSession) HandleWelcome() {
	greetingText := s.GetRandomGreeting()
	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	ctx := s.clientState.AfterAsrSessionCtx.Get(sessionCtx)

	s.ttsManager.EnqueueTtsStart(s.clientState.Ctx)
	s.ttsManager.handleTts(ctx, s.ttsManager.currentAudioGeneration(), llm_common.LLMResponseStruct{Text: greetingText}, nil, nil)
	s.ttsManager.EnqueueTtsStop(s.clientState.Ctx)

	s.clientState.IsWelcomeSpeaking = true
}

func (a *ChatSession) checkExitWords(text string) bool {
	exitWords := []string{"再见", "退下吧", "退出", "退出对话"}
	for _, word := range exitWords {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func (s *ChatSession) GetRandomGreeting() string {
	greetingList := viper.GetStringSlice("greeting_list")
	if len(greetingList) == 0 {
		return "你好，有啥好玩的."
	}
	rand.Seed(time.Now().UnixNano())
	return greetingList[rand.Intn(len(greetingList))]
}

func (s *ChatSession) AddTextToTTSQueue(text string) error {
	s.llmManager.AddTextToTTSQueue(text)
	return nil
}

// InterruptAndClearTTSQueue 触发 TTS 打断并清空发送队列（供 realtime 模式 VAD 打断等场景调用）
func (s *ChatSession) InterruptAndClearTTSQueue() {
	s.ttsManager.InterruptAndClearQueue()
}

// handleAbortMessage 处理中止消息
func (s *ChatSession) HandleAbortMessage(msg *ClientMessage) error {
	// 设置打断状态
	s.clientState.Abort = true

	s.StopSpeaking(true)

	// 记录日志
	log.Infof("设备 %s abort 会话", msg.DeviceID)
	return nil
}

// handleIoTMessage 处理物联网消息
func (s *ChatSession) HandleIoTMessage(msg *ClientMessage) error {
	// 获取客户端状态
	//sessionID := clientState.SessionID

	// 验证设备ID
	/*
		if _, err := s.authManager.GetSession(msg.DeviceID); err != nil {
			return fmt.Errorf("会话验证失败: %v", err)
		}*/

	// 发送 IoT 响应
	err := s.serverTransport.SendIot(msg)
	if err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}

	// 记录日志
	log.Infof("设备 %s 物联网指令: %s", msg.DeviceID, msg.Text)
	return nil
}

func (s *ChatSession) HandleMcpMessage(msg *ClientMessage) error {
	mcpSession := mcp.GetDeviceMcpClient(s.clientState.DeviceID)
	if mcpSession != nil {
		select {
		case <-s.ctx.Done():
			return nil
		default:
			return s.serverTransport.HandleMcpMessage(msg.PayLoad)
		}
	}
	return nil
}

// 释放udp资源
func (s *ChatSession) HandleGoodByeMessage(msg *ClientMessage) error {
	s.serverTransport.transport.CloseAudioChannel()
	return nil
}

func (s *ChatSession) CheckDeviceActivated() (bool, error) {
	if viper.GetBool("auth.enable") {
		if !s.clientState.IsActivated {
			configProvider, err := user_config.GetProvider(viper.GetString("config_provider.type"))
			if err != nil {
				log.Errorf("获取配置提供者失败: %v", err)
				return false, err
			}
			//调用接口再次确认激活状态
			isActivated, err := configProvider.IsDeviceActivated(s.clientState.Ctx, s.clientState.DeviceID, "client_id")
			if err != nil {
				log.Errorf("获取激活状态失败: %v", err)
				return false, err
			}
			if isActivated {
				s.clientState.IsActivated = true
			} else {
				s.HandleNotActivated()
				return false, nil
			}
		}
	}
	return true, nil
}

func (s *ChatSession) HandleListenStart(msg *ClientMessage) error {
	isActivated, err := s.CheckDeviceActivated()
	if err != nil {
		log.Errorf("检查设备激活状态失败: %v", err)
		return err
	}
	if !isActivated {
		return nil
	}

	// 处理拾音模式
	if msg.Mode != "" {
		s.clientState.ListenMode = msg.Mode
		log.Infof("设备 %s 拾音模式: %s", msg.DeviceID, msg.Mode)
	}
	//if s.clientState.ListenMode == "manual" {
	s.StopSpeaking(false)
	//}

	return s.OnListenStart()
}

func (s *ChatSession) HandleListenStop() error {
	/*if s.clientState.ListenMode == "auto" {
		s.clientState.CancelSessionCtx()
	}*/

	//调用
	s.clientState.OnManualStop()

	return nil
}

func (s *ChatSession) OnListenStart() error {
	log.Debugf("OnListenStart start")
	defer log.Debugf("OnListenStart end")

	select {
	case <-s.clientState.Ctx.Done():
		log.Debugf("OnListenStart Ctx done, return")
		return nil
	default:
	}

	s.clientState.Destroy()

	s.clientState.SetStatus(ClientStatusListening)

	ctx := s.clientState.SessionCtx.Get(s.clientState.Ctx)

	//初始化asr相关
	if s.clientState.ListenMode == "manual" {
		s.clientState.VoiceStatus.SetClientHaveVoice(true)
	}

	// 启动asr流式识别，复用 restartAsrRecognition 函数
	err := s.asrManager.RestartAsrRecognition(ctx)
	if err != nil {
		log.Errorf("asr流式识别失败: %v", err)
		s.Close()
		return err
	}

	// 定义消息保存回调
	onMessageSave := func(userMsg *schema.Message, messageID string, audioData []float32) {
		// ASR 文本和音频同时获取，一次性保存（不需要两阶段）
		eventbus.Get().Publish(eventbus.TopicAddMessage, &eventbus.AddMessageEvent{
			ClientState: s.clientState,
			Msg:         *userMsg,
			MessageID:   messageID,
			AudioData:   [][]byte{util.Float32SliceToBytes(audioData)}, // 转换为字节数组
			AudioSize:   len(audioData) * 4,                            // float32 = 4 bytes
			SampleRate:  s.clientState.InputAudioFormat.SampleRate,
			Channels:    s.clientState.InputAudioFormat.Channels,
			IsUpdate:    false, // 一次性保存（文本+音频）
			Timestamp:   time.Now(),
		})
	}

	// 定义错误处理回调
	onError := func(err error) {
		log.Errorf("ASR识别循环错误: %v", err)
		s.Close()
	}

	// 启动ASR识别结果处理循环（资源管理在 ASRManager 内部）
	s.asrManager.StartAsrRecognitionLoop(ctx, onMessageSave, onError)

	return nil
}

// startChat 开始对话
func (s *ChatSession) AddAsrResultToQueue(text string, speakerResult *speaker.IdentifyResult) error {
	log.Debugf("AddAsrResultToQueue text: %s", text)
	if speakerResult != nil && speakerResult.Identified {
		log.Debugf("AddAsrResultToQueue speaker: %s (confidence: %.2f)", speakerResult.SpeakerName, speakerResult.Confidence)
	}
	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	item := AsrResponseChannelItem{
		ctx:           s.clientState.AfterAsrSessionCtx.Get(sessionCtx),
		text:          text,
		speakerResult: speakerResult,
	}
	err := s.chatTextQueue.Push(item)
	if err != nil {
		log.Warnf("chatTextQueue 已满或已关闭, 丢弃消息")
	}
	return nil
}

func (s *ChatSession) processChatText(ctx context.Context) {
	log.Debugf("processChatText start")
	defer log.Debugf("processChatText end")

	for {
		item, err := s.chatTextQueue.Pop(ctx, 0)
		if err != nil {
			if err == util.ErrQueueCtxDone {
				return
			}
			continue
		}

		err = s.actionDoChat(item.ctx, item.text, item.speakerResult)
		if err != nil {
			log.Errorf("处理对话失败: %v", err)
			continue
		}
	}
}

func (s *ChatSession) ClearChatTextQueue() {
	s.chatTextQueue.Clear()
}

// DoExitChat 执行退出聊天逻辑（发送再见语并关闭会话）
func (s *ChatSession) DoExitChat() {
	// 友好的再见语
	goodbyeText := "好的，再见！期待下次与您聊天～"

	// 保存一条 assistant 角色的消息
	goodbyeMsg := schema.AssistantMessage(goodbyeText, nil)
	if err := s.llmManager.AddLlmMessage(s.clientState.Ctx, goodbyeMsg); err != nil {
		log.Errorf("保存再见消息失败: %v", err)
	}

	// 获取 context
	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	ctx := s.clientState.AfterAsrSessionCtx.Get(sessionCtx)

	// 发送 TTS 再见语
	s.ttsManager.EnqueueTtsStart(ctx)

	err := s.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{
		Text:    goodbyeText,
		IsStart: true,
		IsEnd:   true,
	}, true) // 同步处理，等待TTS完成

	if err != nil {
		log.Errorf("发送再见语失败: %v", err)
	}

	s.ttsManager.EnqueueTtsStop(ctx)
	// 关闭会话
	s.Close()
}

func (s *ChatSession) Close() {
	s.closeOnce.Do(func() {
		// 清理ASR资源（资源管理在 ASRManager 内部）
		if s.asrManager != nil {
			s.asrManager.Cleanup()
		}
		deviceID := ""
		if s.clientState != nil {
			deviceID = s.clientState.DeviceID
		}
		log.Debugf("ChatSession.Close() 开始清理会话资源, 设备 %s", deviceID)

		// 取消会话级别的上下文
		if s.cancel != nil {
			s.cancel()
		}

		// 清理聊天文本队列
		s.ClearChatTextQueue()

		// 停止说话和清理音频相关资源
		s.StopSpeaking(true)

		// 关闭服务端传输
		if s.serverTransport != nil {
			s.serverTransport.Close()
		}

		if s.speakerManager != nil {
			s.speakerManager.Close()
		}

		if s.clientState != nil {
			eventbus.Get().Publish(eventbus.TopicSessionEnd, s.clientState)
		}

		log.Debugf("ChatSession.Close() 会话资源清理完成, 设备 %s", deviceID)
	})
}

func (s *ChatSession) actionDoChat(ctx context.Context, text string, speakerResult *speaker.IdentifyResult) error {
	select {
	case <-ctx.Done():
		log.Debugf("actionDoChat ctx done, return")
		return nil
	default:
	}

	if s.checkExitWords(text) {
		// 发布退出聊天事件
		eventbus.Get().Publish(eventbus.TopicExitChat, &eventbus.ExitChatEvent{
			ClientState: s.clientState,
			Reason:      "用户主动退出",
			TriggerType: "exit_words",
			UserText:    text,
			Timestamp:   time.Now(),
		})
		return nil
	}

	clientState := s.clientState

	sessionID := clientState.SessionID

	// 声纹识别后动态切换TTS（未识别到时恢复默认TTS）
	if err := s.switchTTSForSpeaker(speakerResult); err != nil {
		log.Warnf("切换TTS失败: %v", err)
		// 不中断流程，继续使用当前TTS
	}

	// 直接创建Eino原生消息
	userMessage := &schema.Message{
		Role:    schema.User,
		Content: text,
	}

	// 获取全局MCP工具列表
	mcpTools, err := mcp.GetToolsByDeviceId(clientState.DeviceID, clientState.AgentID, clientState.DeviceConfig.MCPServiceNames)
	if err != nil {
		log.Errorf("获取设备 %s 的工具失败: %v", clientState.DeviceID, err)
		mcpTools = make(map[string]tool.InvokableTool)
	}
	if !hasAvailableKnowledgeBase(clientState.DeviceConfig.KnowledgeBases) {
		if _, ok := mcpTools["search_knowledge"]; ok {
			delete(mcpTools, "search_knowledge")
			log.Infof("设备 %s 未关联可用知识库，已移除工具 search_knowledge", clientState.DeviceID)
		}
	}

	// 将MCP工具转换为接口格式以便传递给转换函数
	mcpToolsInterface := make(map[string]interface{})
	for name, tool := range mcpTools {
		mcpToolsInterface[name] = tool
	}

	// 转换MCP工具为Eino ToolInfo格式
	einoTools, err := llm.ConvertMCPToolsToEinoTools(ctx, mcpToolsInterface)
	if err != nil {
		log.Errorf("转换MCP工具失败: %v", err)
		einoTools = nil
	}

	toolNameList := make([]string, 0)
	for _, tool := range einoTools {
		toolNameList = append(toolNameList, tool.Name)
	}

	// 发送带工具的LLM请求
	log.Infof("使用 %d 个MCP工具发送LLM请求, tools: %+v", len(einoTools), toolNameList)

	err = s.llmManager.DoLLmRequest(ctx, userMessage, einoTools, true, speakerResult)
	if err != nil {
		log.Errorf("发送带工具的 LLM 请求失败, seesionID: %s, error: %v", sessionID, err)
		return fmt.Errorf("发送带工具的 LLM 请求失败: %v", err)
	}
	return nil
}

func hasAvailableKnowledgeBase(knowledgeBases []types.KnowledgeBaseRef) bool {
	for _, kb := range knowledgeBases {
		if strings.EqualFold(strings.TrimSpace(kb.Status), "inactive") {
			continue
		}
		if strings.TrimSpace(kb.ExternalKBID) == "" {
			continue
		}
		return true
	}
	return false
}

// switchTTSForSpeaker 为识别的说话人切换TTS
func (s *ChatSession) switchTTSForSpeaker(speakerResult *speaker.IdentifyResult) error {
	s.clientState.SpeakerTTSConfig = nil

	// 1. 检查 speakerResult 是否为 nil
	if speakerResult == nil {
		log.Debug("speakerResult 为 nil，清空声纹TTS配置")
		return nil
	}

	// 2. 查找声纹组配置
	speakerGroupInfo, found := s.clientState.DeviceConfig.VoiceIdentify[speakerResult.SpeakerName]
	if !found {
		// 未找到配置，清空声纹TTS配置
		log.Debugf("未找到声纹组 %s 的配置，清空声纹TTS配置", speakerResult.SpeakerName)
		return nil
	}

	// 3. 检查是否配置了自定义音色
	if speakerGroupInfo.TTSConfigID == nil || *speakerGroupInfo.TTSConfigID == "" {
		// 未配置自定义音色，清空声纹TTS配置
		log.Debugf("声纹组 %s 未配置自定义TTS，清空声纹TTS配置", speakerResult.SpeakerName)
		return nil
	}

	// 4. 从系统配置（viper）中查找对应的TTS配置
	var targetTTSConfig *types.TtsConfigItem
	ttsConfigsRaw := viper.Get("tts")
	if ttsConfigsRaw == nil {
		return fmt.Errorf("系统配置中未找到 tts")
	}

	// 解析 tts 配置（现在是一个 map，key 是 config_id）
	if ttsConfigsMap, ok := ttsConfigsRaw.(map[string]interface{}); ok {
		// 查找匹配的 config_id
		if configItem, exists := ttsConfigsMap[*speakerGroupInfo.TTSConfigID]; exists {
			if configMap, ok := configItem.(map[string]interface{}); ok {
				// 解析配置项
				ttsItem := &types.TtsConfigItem{
					ConfigID: *speakerGroupInfo.TTSConfigID,
				}
				if name, ok := configMap["name"].(string); ok {
					ttsItem.Name = name
				}
				if provider, ok := configMap["provider"].(string); ok {
					ttsItem.Provider = provider
				}
				if isDefault, ok := configMap["is_default"].(bool); ok {
					ttsItem.IsDefault = isDefault
				}
				// 配置项的其他字段直接作为 config
				ttsItem.Config = make(map[string]interface{})
				for k, v := range configMap {
					if k != "name" && k != "provider" && k != "is_default" && k != "config_id" {
						ttsItem.Config[k] = v
					}
				}
				targetTTSConfig = ttsItem
			}
		}
	}

	if targetTTSConfig == nil {
		return fmt.Errorf("未找到TTS配置 %s", *speakerGroupInfo.TTSConfigID)
	}

	// 5. 复制TTS配置以避免修改原始配置
	ttsConfig := make(map[string]interface{})
	for k, v := range targetTTSConfig.Config {
		ttsConfig[k] = v
	}

	// 6. 如果配置了音色值，覆盖到TTS配置中
	if speakerGroupInfo.Voice != nil && *speakerGroupInfo.Voice != "" {
		// 根据provider设置对应的音色字段
		if targetTTSConfig.Provider == "cosyvoice" {
			ttsConfig["spk_id"] = *speakerGroupInfo.Voice
		} else {
			ttsConfig["voice"] = *speakerGroupInfo.Voice
		}
		log.Debugf("为说话人 %s 设置音色: %s", speakerResult.SpeakerName, *speakerGroupInfo.Voice)
	}
	if targetTTSConfig.Provider == "aliyun_qwen" &&
		speakerGroupInfo.VoiceModelOverride != nil &&
		strings.TrimSpace(*speakerGroupInfo.VoiceModelOverride) != "" {
		overrideModel := strings.TrimSpace(*speakerGroupInfo.VoiceModelOverride)
		ttsConfig["model"] = overrideModel
		log.Debugf("为说话人 %s 覆盖千问模型: %s", speakerResult.SpeakerName, overrideModel)
	}

	// 7. 保存完整的 TTS 配置（深拷贝）
	s.clientState.SpeakerTTSConfig = make(map[string]interface{})
	for k, v := range ttsConfig {
		s.clientState.SpeakerTTSConfig[k] = v
	}
	// 确保 provider 在 config 中
	s.clientState.SpeakerTTSConfig["provider"] = targetTTSConfig.Provider

	log.Infof("✅ 为说话人 %s 切换TTS配置成功 - Provider: %s, ConfigID: %s, Voice: %v",
		speakerResult.SpeakerName,
		targetTTSConfig.Provider,
		targetTTSConfig.ConfigID,
		speakerGroupInfo.Voice)

	return nil
}
