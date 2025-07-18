package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"runtime/debug"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	"xiaozhi-esp32-server-golang/internal/app/server/auth"
	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	. "xiaozhi-esp32-server-golang/internal/data/msg"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_memory "xiaozhi-esp32-server-golang/internal/domain/llm/memory"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

type AsrResponseChannelItem struct {
	ctx  context.Context
	text string
}

type ChatSession struct {
	clientState     *ClientState
	asrManager      *ASRManager
	ttsManager      *TTSManager
	llmManager      *LLMManager
	serverTransport *ServerTransport

	ctx    context.Context
	cancel context.CancelFunc

	chatTextQueue *util.Queue[AsrResponseChannelItem]
}

type ChatSessionOption func(*ChatSession)

func WithASRManager(asr *ASRManager) ChatSessionOption {
	return func(s *ChatSession) {
		s.asrManager = asr
	}
}

func WithTTSManager(tts *TTSManager) ChatSessionOption {
	return func(s *ChatSession) {
		s.ttsManager = tts
	}
}

func WithServerTransport(serverTransport *ServerTransport) ChatSessionOption {
	return func(s *ChatSession) {
		s.serverTransport = serverTransport
	}
}

func WithLLMManager(llm *LLMManager) ChatSessionOption {
	return func(s *ChatSession) {
		s.llmManager = llm
	}
}

func NewChatSession(pctx context.Context, clientState *ClientState, opts ...ChatSessionOption) *ChatSession {
	ctx, cancel := context.WithCancel(pctx)
	s := &ChatSession{
		clientState:   clientState,
		ctx:           ctx,
		cancel:        cancel,
		chatTextQueue: util.NewQueue[AsrResponseChannelItem](10),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *ChatSession) Start(ctx context.Context) error {
	err := s.InitAsrLlmTts()
	if err != nil {
		log.Errorf("初始化ASR/LLM/TTS失败: %v", err)
		return err
	}

	go s.CmdMessageLoop(ctx)   //处理信令消息
	go s.AudioMessageLoop(ctx) //处理音频数据
	go s.processChatText(ctx)  //处理 asr后 的对话消息
	go s.llmManager.Start(ctx) //处理 llm后 的一系列返回消息
	go s.ttsManager.Start(ctx) //处理 tts的 消息队列

	return nil
}

// 在mqtt 收到type: listen, state: start后进行
func (c *ChatSession) InitAsrLlmTts() error {
	ttsConfig := c.clientState.DeviceConfig.Tts
	ttsProvider, err := tts.GetTTSProvider(ttsConfig.Provider, ttsConfig.Config)
	if err != nil {
		return fmt.Errorf("创建 TTS 提供者失败: %v", err)
	}
	c.clientState.TTSProvider = ttsProvider

	if err := c.clientState.InitLlm(); err != nil {
		return fmt.Errorf("初始化LLM失败: %v", err)
	}
	if err := c.clientState.InitAsr(); err != nil {
		return fmt.Errorf("初始化ASR失败: %v", err)
	}
	c.clientState.SetAsrPcmFrameSize(c.clientState.InputAudioFormat.SampleRate, c.clientState.InputAudioFormat.Channels, c.clientState.InputAudioFormat.FrameDuration)

	return nil
}

func (c *ChatSession) CmdMessageLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Infof("设备 %s recvCmd context cancel", c.clientState.DeviceID)
			return
		default:
		}

		message, err := c.serverTransport.RecvCmd(120)
		if err != nil {
			log.Errorf("recv cmd error: %v", err)
			return
		}
		log.Infof("收到文本消息: %s", string(message))
		if err := c.HandleTextMessage(message); err != nil {
			log.Errorf("处理文本消息失败: %v", err)
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
		message, err := c.serverTransport.RecvAudio(300)
		if err != nil {
			log.Errorf("recv audio error: %v", err)
			return
		}
		log.Debugf("收到音频数据，大小: %d 字节", len(message))
		if c.clientState.GetClientVoiceStop() {
			//log.Debug("客户端停止说话, 跳过音频数据")
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

	 c.clientState.OpusAudioBuffer <- data
	
	if len(c.clientState.OpusAudioBuffer) >= 100 {
		<-c.clientState.OpusAudioBuffer //丢包
		log.Warnf("音频缓冲区已满, 丢弃音频数据")
	}
	return true
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
	clientState.SetAsrPcmFrameSize(clientState.InputAudioFormat.SampleRate, clientState.InputAudioFormat.Channels, clientState.InputAudioFormat.FrameDuration)

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
	// 唤醒词检测
	s.StopSpeaking(false)

	// 如果有文本，处理唤醒词
	if msg.Text != "" {
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
					greetingText := s.GetRandomGreeting()
					s.AddTextToTTSQueue(greetingText)
					s.clientState.IsWelcomeSpeaking = true
				}
			} else {
				//进行llm->tts聊天
				if err := s.AddAsrResultToQueue(text); err != nil {
					log.Errorf("开始对话失败: %v", err)
				}
			}
		}
	}
	return nil
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

// handleAbortMessage 处理中止消息
func (s *ChatSession) HandleAbortMessage(msg *ClientMessage) error {
	// 设置打断状态
	s.clientState.Abort = true
	s.clientState.Dialogue.Messages = nil // 清空对话历史

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
		case s.serverTransport.McpRecvMsgChan <- msg.PayLoad:
		default:
			log.Warnf("mcp 接收消息通道已满, 丢弃消息")
		}
	}
	return nil
}

// 释放udp资源
func (s *ChatSession) HandleGoodByeMessage(msg *ClientMessage) error {
	s.serverTransport.transport.CloseAudioChannel()
	return nil
}

func (s *ChatSession) HandleListenStart(msg *ClientMessage) error {
	// 处理拾音模式
	if msg.Mode != "" {
		s.clientState.ListenMode = msg.Mode
		log.Infof("设备 %s 拾音模式: %s", msg.DeviceID, msg.Mode)
	}
	if s.clientState.ListenMode == "manual" {
		s.StopSpeaking(false)
	}
	s.clientState.SetStatus(ClientStatusListening)

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

	ctx := s.clientState.GetSessionCtx()

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

	// 启动一个goroutine处理asr结果
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("asr结果处理goroutine panic: %v, stack: %s", r, string(debug.Stack()))
			}
		}()

		//最大空闲 60s

		var startIdleTime, maxIdleTime int64
		startIdleTime = time.Now().Unix()
		maxIdleTime = 60

		for {
			select {
			case <-ctx.Done():
				log.Debugf("asr ctx done")
				return
			default:
			}

			text, err := s.clientState.RetireAsrResult(ctx)
			if err != nil {
				log.Errorf("处理asr结果失败: %v", err)
				return
			}

			//统计asr耗时
			log.Debugf("处理asr结果: %s, 耗时: %d ms", text, s.clientState.GetAsrDuration())

			if text != "" {
				// 重置重试计数器
				startIdleTime = 0

				//当获取到asr结果时, 结束语音输入
				s.clientState.OnVoiceSilence()

				//发送asr消息
				err = s.serverTransport.SendAsrResult(text)
				if err != nil {
					log.Errorf("发送asr消息失败: %v", err)
					return
				}

				err = s.AddAsrResultToQueue(text)
				if err != nil {
					log.Errorf("开始对话失败: %v", err)
					return
				}
				return
			} else {
				select {
				case <-ctx.Done():
					log.Debugf("asr ctx done")
					return
				default:
				}
				log.Debugf("ready Restart Asr, s.clientState.Status: %s", s.clientState.Status)
				if s.clientState.Status == ClientStatusListening || s.clientState.Status == ClientStatusListenStop {
					// text 为空，检查是否需要重新启动ASR
					diffTs := time.Now().Unix() - startIdleTime
					if startIdleTime > 0 && diffTs <= maxIdleTime {
						log.Warnf("ASR识别结果为空，尝试重启ASR识别, diff ts: %s", diffTs)
						if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("重启ASR识别失败: %v", restartErr)
							return
						}
						continue
					} else {
						log.Warnf("ASR识别结果为空，已达到最大空闲时间: %d", maxIdleTime)
						s.Close()
						return
					}
				}
			}
			return
		}
	}()
	return nil
}

// startChat 开始对话
func (s *ChatSession) AddAsrResultToQueue(text string) error {
	log.Debugf("AddAsrResultToQueue text: %s", text)
	item := AsrResponseChannelItem{
		ctx:  s.clientState.GetSessionCtx(),
		text: text,
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

		err = s.actionDoChat(item.ctx, item.text)
		if err != nil {
			log.Errorf("处理对话失败: %v", err)
			continue
		}
	}
}

func (s *ChatSession) ClearChatTextQueue() {
	s.chatTextQueue.Clear()
}

func (s *ChatSession) Close() {
	s.StopSpeaking(true)
	s.cancel()
	s.serverTransport.Close()
}

func (s *ChatSession) actionDoChat(ctx context.Context, text string) error {
	select {
	case <-ctx.Done():
		log.Debugf("actionDoChat ctx done, return")
		return nil
	default:
	}

	//当收到停止说话或退出说话时, 则退出对话
	clearText := strings.TrimSpace(text)
	if clearText == "再见" || clearText == "退下吧" || clearText == "退出" || clearText == "退出对话" || clearText == "停止" || clearText == "停止说话" {
		s.Close()
		return nil
	}

	clientState := s.clientState

	sessionID := clientState.SessionID

	requestMessages, err := llm_memory.Get().GetMessagesForLLM(ctx, clientState.DeviceID, 10)
	if err != nil {
		log.Errorf("获取对话历史失败: %v", err)
	}

	// 直接创建Eino原生消息
	userMessage := &schema.Message{
		Role:    schema.User,
		Content: text,
	}
	requestMessages = append(requestMessages, *userMessage)

	// 添加用户消息到对话历史
	//llm_memory.Get().AddMessage(ctx, clientState.DeviceID, schema.User, text)

	// 直接传递Eino原生消息，无需转换
	requestEinoMessages := make([]*schema.Message, len(requestMessages))
	for i, msg := range requestMessages {
		requestEinoMessages[i] = &msg
	}

	// 获取全局MCP工具列表
	mcpTools, err := mcp.GetToolsByDeviceId(clientState.DeviceID)
	if err != nil {
		log.Errorf("获取设备 %s 的工具失败: %v", clientState.DeviceID, err)
		mcpTools = make(map[string]tool.InvokableTool)
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

	err = s.llmManager.DoLLmRequest(ctx, requestEinoMessages, einoTools, true)
	if err != nil {
		log.Errorf("发送带工具的 LLM 请求失败, seesionID: %s, error: %v", sessionID, err)
		return fmt.Errorf("发送带工具的 LLM 请求失败: %v", err)
	}
	return nil
}
