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
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	"xiaozhi-esp32-server-golang/internal/data/eino"
	vad_types "xiaozhi-esp32-server-golang/internal/data/vad"

	"xiaozhi-esp32-server-golang/internal/app/server/auth"
	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	. "xiaozhi-esp32-server-golang/internal/data/msg"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/memory"
	"xiaozhi-esp32-server-golang/internal/domain/memory/llm_memory"
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

func NewChatSession(clientState *ClientState, serverTransport *ServerTransport, opts ...ChatSessionOption) *ChatSession {
	s := &ChatSession{
		clientState:     clientState,
		serverTransport: serverTransport,
		chatTextQueue:   util.NewQueue[AsrResponseChannelItem](10),
	}
	for _, opt := range opts {
		opt(s)
	}

	s.asrManager = NewASRManager(clientState, serverTransport)
	s.ttsManager = NewTTSManager(clientState, serverTransport)
	s.llmManager = NewLLMManager(clientState, serverTransport, s.ttsManager)

	return s
}

func (s *ChatSession) Start(pctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(pctx)

	err := s.InitAsrLlmTts()
	if err != nil {
		log.Errorf("初始化ASR/LLM/TTS失败: %v", err)
		return err
	}

	err = s.initHistoryMessages()
	if err != nil {
		log.Errorf("初始化对话历史失败: %v", err)
	}

	go s.CmdMessageLoop(s.ctx)   //处理信令消息
	go s.AudioMessageLoop(s.ctx) //处理音频数据
	go s.processChatText(s.ctx)  //处理 asr后 的对话消息
	go s.llmManager.Start(s.ctx) //处理 llm后 的一系列返回消息
	go s.ttsManager.Start(s.ctx) //处理 tts的 消息队列

	return nil
}

// 初始化历史对话记录到内存中
func (s *ChatSession) initHistoryMessages() error {
	historyMessages, err := llm_memory.Get().GetMessages(s.ctx, s.clientState.DeviceID, s.clientState.AgentID, 20)
	if err != nil {
		log.Errorf("获取对话历史失败: %v", err)
		return err
	}
	s.clientState.InitMessages(historyMessages)
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

	memoryConfig := c.clientState.DeviceConfig.Memory
	memoryProvider, err := memory.GetProvider(memory.MemoryType(memoryConfig.Provider), memoryConfig.Config)
	if err != nil {
		return fmt.Errorf("创建 Memory 提供者失败: %v", err)
	}
	c.clientState.MemoryProvider = memoryProvider
	//初始化memory context
	context, err := memoryProvider.GetContext(c.ctx, c.clientState.GetDeviceIDOrAgentID(), 500)
	if err != nil {
		log.Warnf("初始化memory context失败: %v", err)
	}
	c.clientState.MemoryContext = context
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
	closed := c.clientState.OpusAudioBufferWriter.Send(data, nil)
	if closed {
		log.Errorf("opus audio buffer writer is closed, drop audio data")
		return false
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

	//start vad audio process
	//s.asrManager.ProcessVadAudio(s.ctx, s.clientState.OpusAudioBufferReader, vad_types.WithOnClose(s.Close))

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
				if err := s.AddAsrResultToQueue(text); err != nil {
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

	s.serverTransport.SendTtsStart()
	defer s.serverTransport.SendTtsStop()

	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	s.ttsManager.handleTts(s.clientState.AfterAsrSessionCtx.Get(sessionCtx), llm_common.LLMResponseStruct{
		Text: fmt.Sprintf("请在后台添加设备，激活码: %d", code),
	})

}

func (s *ChatSession) HandleWelcome() {
	greetingText := s.GetRandomGreeting()
	s.serverTransport.SendTtsStart()
	defer s.serverTransport.SendTtsStop()

	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	s.ttsManager.handleTts(s.clientState.AfterAsrSessionCtx.Get(sessionCtx), llm_common.LLMResponseStruct{
		Text: greetingText,
	})

	s.clientState.IsWelcomeSpeaking = true
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

	ctx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	_ = ctx

	//初始化asr相关
	if s.clientState.ListenMode == "manual" {
		s.clientState.VoiceStatus.SetClientHaveVoice(true)
	}

	//在这里开始eino编排
	err := s.RunVadAsrGraph(ctx)
	if err != nil {
		log.Errorf("执行VAD+ASR graph失败: %v", err)
		return err
	}

	return nil
}

// CreateVadAsrGraph 创建VAD+ASR的eino graph
func (s *ChatSession) CreateVadAsrGraph(ctx context.Context) (compose.Runnable[[]byte, string], error) {
	// 创建图，输入是 []byte（opus音频），输出是 string（ASR识别结果）
	// 注意：Graph 的类型参数应该是非流式类型，使用 Collect 方法时传入流式输入
	graph := compose.NewGraph[[]byte, string]()

	// 创建VAD节点：将 opus 音频转换为 PCM 音频
	// 需要将非流式输入转换为流式输入，然后调用 ProcessVadAudio
	vadNode := compose.TransformableLambda(func(ctx context.Context, input *schema.StreamReader[[]byte]) (*schema.StreamReader[[]float32], error) {
		return s.asrManager.ProcessVadAudio(ctx, input, vad_types.WithOnClose(s.Close))
	})

	// 创建ASR节点：将 PCM 音频转换为文本
	// 使用 CollectableLambda 因为 EinoAsrComponent 返回非流式的 string
	asrNode := compose.CollectableLambda(s.EinoAsrComponent)

	// 添加节点到图
	_ = graph.AddLambdaNode(eino.NodeVAD, vadNode, compose.WithNodeName(eino.NodeVAD))
	_ = graph.AddLambdaNode(eino.NodeASR, asrNode, compose.WithNodeName(eino.NodeASR))

	// 构建边关系：START -> VAD -> ASR -> END
	// 注意：START 节点输出 []byte，但 VAD 节点需要 *schema.StreamReader[[]byte]
	// eino 框架会自动处理这种转换
	_ = graph.AddEdge(compose.START, eino.NodeVAD)
	_ = graph.AddEdge(eino.NodeVAD, eino.NodeASR)
	_ = graph.AddEdge(eino.NodeASR, compose.END)

	// 编译图
	r, err := graph.Compile(ctx)
	if err != nil {
		log.Errorf("编译VAD+ASR Graph失败: %v", err)
		return nil, err
	}

	return r, nil
}

// RunVadAsrGraph 执行VAD+ASR的eino graph
func (s *ChatSession) RunVadAsrGraph(ctx context.Context) error {
	g, err := s.CreateVadAsrGraph(ctx)
	if err != nil {
		log.Errorf("创建VAD+ASR Graph失败: %v", err)
		return err
	}

	// 执行图，输入是 OpusAudioBufferReader，使用 Collect 方法处理流式输入并返回非流式输出
	// Collect 是阻塞的，会一直等待直到流结束并返回最终结果
	text, err := g.Collect(ctx, s.clientState.OpusAudioBufferReader)
	if err != nil {
		log.Errorf("执行VAD+ASR Graph失败: %v", err)
		return err
	}
	if text != "" {
		log.Debugf("VAD+ASR Graph识别结果: %s", text)
	}

	return nil
}

func (s *ChatSession) EinoAsrComponent(ctx context.Context, audioStream *schema.StreamReader[[]float32]) (string, error) {

	// 启动asr流式识别，复用 restartAsrRecognition 函数
	err := s.asrManager.RestartAsrRecognition(ctx, audioStream)
	if err != nil {
		log.Errorf("asr流式识别失败: %v", err)
		s.Close()
		return "", err
	}

	// 启动一个goroutine处理asr结果
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
			return "", nil
		default:
		}

		text, isRetry, err := s.clientState.RetireAsrResult(ctx)
		if err != nil {
			log.Errorf("处理asr结果失败: %v", err)
			s.Close()
			return "", err
		}
		if !isRetry {
			log.Debugf("asrResult is not retry, return")
			return "", nil
		}

		//统计asr耗时
		log.Debugf("处理asr结果: %s, 耗时: %d ms", text, s.clientState.GetAsrDuration())

		if text != "" {
			//如果是realtime模式下，需要停止 当前的llm和tts
			if s.clientState.IsRealTime() && viper.GetInt("chat.realtime_mode") == 2 {
				s.clientState.AfterAsrSessionCtx.Cancel()
			}
			// 重置重试计数器
			startIdleTime = time.Now().Unix()

			//当获取到asr结果时, 结束语音输入
			s.clientState.OnVoiceSilence()

			//发送asr消息
			err = s.serverTransport.SendAsrResult(text)
			if err != nil {
				log.Errorf("发送asr消息失败: %v", err)
				s.Close()
				return "", err
			}

			err = s.AddAsrResultToQueue(text)
			if err != nil {
				log.Errorf("开始对话失败: %v", err)
				s.Close()
				return "", err
			}
			if s.clientState.IsRealTime() {
				if restartErr := s.asrManager.RestartAsrRecognition(ctx, audioStream); restartErr != nil {
					log.Errorf("重启ASR识别失败: %v", restartErr)
					s.Close()
					return "", restartErr
				}
				//realtime模式下, 继续重启asr识别
				continue
			}
			return text, nil
		} else {
			select {
			case <-ctx.Done():
				log.Debugf("asr ctx done")
				return "", nil
			default:
			}
			log.Debugf("ready Restart Asr, s.clientState.Status: %s", s.clientState.Status)
			if s.clientState.Status == ClientStatusListening || s.clientState.Status == ClientStatusListenStop {
				// text 为空，检查是否需要重新启动ASR
				diffTs := time.Now().Unix() - startIdleTime
				if startIdleTime > 0 && diffTs <= maxIdleTime {
					log.Warnf("ASR识别结果为空，尝试重启ASR识别, diff ts: %s", diffTs)
					if restartErr := s.asrManager.RestartAsrRecognition(ctx, audioStream); restartErr != nil {
						log.Errorf("重启ASR识别失败: %v", restartErr)
						s.Close()
						return "", restartErr
					}
					continue
				} else {
					log.Warnf("ASR识别结果为空，已达到最大空闲时间: %d", maxIdleTime)
					s.Close()
					return "", nil
				}
			}
		}
		return "", nil
	}
}

// startChat 开始对话
func (s *ChatSession) AddAsrResultToQueue(text string) error {
	log.Debugf("AddAsrResultToQueue text: %s", text)
	sessionCtx := s.clientState.SessionCtx.Get(s.clientState.Ctx)
	item := AsrResponseChannelItem{
		ctx:  s.clientState.AfterAsrSessionCtx.Get(sessionCtx),
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

		//err = s.actionDoChat(item.ctx, item.text)
		err = s.RunEinoGraph(item.ctx, item.text)
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
	deviceID := ""
	if s.clientState != nil {
		deviceID = s.clientState.DeviceID
	}
	log.Debugf("ChatSession.Close() 开始清理会话资源, 设备 %s", deviceID)

	// 停止说话和清理音频相关资源
	s.StopSpeaking(true)

	// 清理聊天文本队列
	s.ClearChatTextQueue()

	// 关闭服务端传输
	if s.serverTransport != nil {
		s.serverTransport.Close()
	}

	// 取消会话级别的上下文
	s.cancel()

	if s.clientState != nil {
		eventbus.Get().Publish(eventbus.TopicSessionEnd, s.clientState)
	}

	log.Debugf("ChatSession.Close() 会话资源清理完成, 设备 %s", deviceID)
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
	exitWords := []string{"再见", "退下吧", "退出", "退出对话", "停止", "停止说话"}
	for _, word := range exitWords {
		if strings.Contains(clearText, word) {
			s.Close()
			return nil
		}
	}

	clientState := s.clientState

	sessionID := clientState.SessionID

	// 直接创建Eino原生消息
	userMessage := &schema.Message{
		Role:    schema.User,
		Content: text,
	}

	// 获取全局MCP工具列表
	mcpTools, err := mcp.GetToolsByDeviceId(clientState.DeviceID, clientState.AgentID)
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

	err = s.llmManager.DoLLmRequest(ctx, userMessage, einoTools, true)
	if err != nil {
		log.Errorf("发送带工具的 LLM 请求失败, seesionID: %s, error: %v", sessionID, err)
		return fmt.Errorf("发送带工具的 LLM 请求失败: %v", err)
	}
	return nil
}
