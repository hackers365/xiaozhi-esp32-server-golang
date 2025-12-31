package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"sync"

	"xiaozhi-esp32-server-golang/internal/domain/asr"
	utypes "xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	llm_types "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/memory"
	"xiaozhi-esp32-server-golang/internal/domain/tts"

	. "xiaozhi-esp32-server-golang/internal/data/audio"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

// Dialogue 表示对话历史
type Dialogue struct {
	Messages []*schema.Message
}

const (
	ClientStatusInit       = "init"
	ClientStatusListening  = "listening"
	ClientStatusListenStop = "listenStop"
	ClientStatusLLMStart   = "llmStart"
	ClientStatusTTSStart   = "ttsStart"
)

type SendAudioData func(audioData []byte) error

// ClientState 表示客户端状态
type ClientState struct {
	IsActivated bool
	// 对话历史
	Dialogue *Dialogue
	// 打断状态
	Abort bool
	// 拾音模式
	ListenMode string
	// 设备ID
	DeviceID string
	AgentID  string
	// 会话ID
	SessionID string

	//设备配置
	DeviceConfig utypes.UConfig

	Vad
	Asr
	Llm

	// TTS 提供者
	TTSProvider tts.TTSProvider
	// memory提供者
	MemoryProvider memory.MemoryProvider
	MemoryContext  string //memory context

	// 上下文控制
	Ctx    context.Context
	Cancel context.CancelFunc

	SessionCtx         Ctx //一次对话的上下文
	AfterAsrSessionCtx Ctx //asr后流程的上下文

	//prompt, 系统提示词
	SystemPrompt string

	InputAudioFormat  AudioFormat //输入音频格式
	OutputAudioFormat AudioFormat //输出音频格式

	// opus接收的音频数据缓冲区
	OpusAudioBufferWriter *schema.StreamWriter[[]byte]
	OpusAudioBufferReader *schema.StreamReader[[]byte]

	// pcm接收的音频数据缓冲区
	AsrAudioBuffer *AsrAudioBuffer

	VoiceStatus

	UdpSendAudioData SendAudioData //发送音频数据
	Statistic        Statistic     //耗时统计
	MqttLastActiveTs int64         //最后活跃时间
	VadLastActiveTs  int64         //vad最后活跃时间, 超过 60s && 没有在tts则断开连接

	Status string //状态 listening, llmStart, ttsStart

	IsTtsStart        bool //是否tts开始
	IsWelcomeSpeaking bool //是否已经欢迎语
}

func (c *ClientState) IsRealTime() bool {
	return c.ListenMode == "realtime"
}

func (c *ClientState) GetDeviceIDOrAgentID() string {
	if c.AgentID != "" {
		return c.AgentID
	}
	return c.DeviceID
}

// 历史消息相关的方法开始
func (c *ClientState) AddMessage(msg *schema.Message) {
	if msg == nil {
		log.Warnf("尝试添加 nil 消息到对话历史")
		return
	}
	c.Dialogue.Messages = append(c.Dialogue.Messages, msg)
}

func (c *ClientState) GetMessages(count int) []*schema.Message {
	// 添加边界检查，防止数组越界
	if len(c.Dialogue.Messages) == 0 {
		return []*schema.Message{}
	}

	// 计算起始索引，确保不会越界
	startIndex := len(c.Dialogue.Messages) - count
	if startIndex < 0 {
		startIndex = 0
	}

	return AlignToolMessages(c.Dialogue.Messages[startIndex:])
}

/*
func AlignMessage(messages []*schema.Message) []*schema.Message {
	findMsgTypeUser := false
	// 为保证消息完整性, 遍历 找到第一个User之后的消息
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg == nil {
			continue
		}
		if !findMsgTypeUser {
			if msg.Role == schema.User {
				return messages[i:]
			}
			continue
		}
	}
	return messages
}
*/
// AlignToolMessages 保证 role:tool 消息中的 tool_call_id 与 role:assistant 消息中的 tool_calls 的 id 对应
// 如果不匹配则删除对应的 tool 消息，同时处理反向不匹配的场景
func AlignToolMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}

	// 收集所有 assistant 消息中的 tool_calls id
	validToolCallIDs := make(map[string]bool)
	// 收集所有 tool 消息中的 tool_call_id
	usedToolCallIDs := make(map[string]bool)

	// 第一遍遍历：收集 assistant 消息中的 tool_calls id 和 tool 消息中的 tool_call_id
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				if toolCall.ID != "" {
					validToolCallIDs[toolCall.ID] = true
				}
			}
		}

		if msg.Role == schema.Tool && msg.ToolCallID != "" {
			usedToolCallIDs[msg.ToolCallID] = true
		}
	}

	// 过滤消息，处理双向不匹配的情况
	var alignedMessages []*schema.Message
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		// 如果是 tool 消息，检查 tool_call_id 是否有效
		if msg.Role == schema.Tool {
			if msg.ToolCallID != "" && validToolCallIDs[msg.ToolCallID] {
				alignedMessages = append(alignedMessages, msg)
			}
		} else if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			// 处理 assistant 消息，检查是否有未使用的 tool_calls
			for _, toolCall := range msg.ToolCalls {
				if toolCall.ID != "" {
					if usedToolCallIDs[toolCall.ID] {
						alignedMessages = append(alignedMessages, msg)
					} else {
						continue
					}
				}
			}
		} else {
			// 其他类型的消息直接保留
			alignedMessages = append(alignedMessages, msg)
		}
	}

	return alignedMessages
}

func (c *ClientState) InitMessages(messages []*schema.Message) error {
	c.Dialogue.Messages = AlignToolMessages(messages)
	return nil
}

//历史消息相关的方法结束

func (c *ClientState) SetTtsStart(isStart bool) {
	c.IsTtsStart = isStart
}

func (c *ClientState) GetTtsStart() bool {
	return c.IsTtsStart
}

func (c *ClientState) GetMaxIdleDuration() int64 {
	maxIdleDuration := viper.GetInt64("chat.max_idle_duration")
	if maxIdleDuration == 0 {
		maxIdleDuration = 20000
	}
	return maxIdleDuration
}

func (c *ClientState) UpdateLastActiveTs() {
	c.MqttLastActiveTs = time.Now().Unix()
}

func (c *ClientState) IsActive() bool {
	diff := time.Now().Unix() - c.MqttLastActiveTs
	return c.MqttLastActiveTs > 0 && diff <= ClientActiveTs
}

func (c *ClientState) SetStatus(status string) {
	c.Status = status
}

func (c *ClientState) GetStatus() string {
	return c.Status
}

type Ctx struct {
	sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *Ctx) Reset() {
	c.Lock()
	defer c.Unlock()
	if c.ctx != nil {
		c.cancel()
		c.ctx = nil
	}
}

func (c *Ctx) Get(parentCtx context.Context) context.Context {
	c.Lock()
	defer c.Unlock()
	if c.ctx == nil {
		c.ctx, c.cancel = context.WithCancel(parentCtx)
	}
	return c.ctx
}

func (c *Ctx) Cancel() {
	c.Lock()
	defer c.Unlock()
	if c.ctx != nil {
		c.cancel()
		c.ctx = nil
	}
}

func (s *ClientState) getLLMProvider() (llm_types.LLMProvider, error) {
	llmConfig := s.DeviceConfig.Llm
	llmType, ok := llmConfig.Config["type"]
	if !ok {
		log.Errorf("getLLMProvider err: not found llm type: %+v", llmConfig)
		return nil, fmt.Errorf("llm config type not found")
	}
	llmProvider, err := llm.GetLLMProvider(llmType.(string), llmConfig.Config)
	if err != nil {
		return nil, fmt.Errorf("创建 LLM 提供者失败: %v", err)
	}
	return llmProvider, nil
}

func (s *ClientState) InitLlm() error {
	ctx, cancel := context.WithCancel(s.Ctx)

	llmProvider, err := s.getLLMProvider()
	if err != nil {
		log.Errorf("创建 LLM 提供者失败: %v", err)
		return err
	}

	s.Llm = Llm{
		Ctx:         ctx,
		Cancel:      cancel,
		LLMProvider: llmProvider,
	}
	return nil
}

func (s *ClientState) InitAsr() error {
	asrConfig := s.DeviceConfig.Asr

	log.Infof("初始化asr, asrConfig: %+v", asrConfig)

	//初始化asr
	asrProvider, err := asr.NewAsrProvider(asrConfig.Provider, asrConfig.Config)
	if err != nil {
		log.Errorf("创建asr提供者失败: %v", err)
		return fmt.Errorf("创建asr提供者失败: %v", err)
	}
	ctx, cancel := context.WithCancel(s.Ctx)
	s.Asr = Asr{
		Ctx:             ctx,
		Cancel:          cancel,
		AsrProvider:     asrProvider,
		AsrAudioChannel: make(chan []float32, 100),
		AsrEnd:          make(chan bool, 1),
		AsrResult:       bytes.Buffer{},
	}

	if rawAutoEnd, ok := asrConfig.Config["auto_end"]; ok {
		if autoEnd, ok := rawAutoEnd.(bool); ok {
			s.Asr.AutoEnd = autoEnd
		}
	}
	return nil
}

func (c *ClientState) Destroy() {
	c.Asr.Stop()
	c.Vad.Reset()

	c.VoiceStatus.Reset()
	c.AsrAudioBuffer.ClearAsrAudioData()

	c.SessionCtx.Reset()
	c.AfterAsrSessionCtx.Reset()

	c.Statistic.Reset()
	c.SetStatus(ClientStatusInit)
	c.SetTtsStart(false)
}

func (c *ClientState) SetAsrPcmFrameSize(sampleRate int, channels int, perFrameDuration int) {
	c.AsrAudioBuffer.PcmFrameSize = sampleRate * channels * perFrameDuration / 1000
}

func (state *ClientState) OnManualStop() {
	state.OnVoiceSilence()
}

func (state *ClientState) OnVoiceSilence() {
	state.SetClientVoiceStop(true) //设置停止说话标志位, 此时收到的音频数据不会进vad
	//客户端停止说话
	state.Asr.Stop() //停止asr并获取结果，进行llm
	//释放vad
	state.Vad.Reset() //释放vad实例
	//asr统计
	state.SetStartAsrTs() //进行asr统计

	state.SetStatus(ClientStatusListenStop)
}

type Llm struct {
	Ctx    context.Context
	Cancel context.CancelFunc
	// LLM 提供者
	LLMProvider llm_types.LLMProvider
	//asr to text接收的通道
	LLmRecvChannel chan llm_common.LLMResponseStruct
}

// ClientMessage 表示客户端消息
type ClientMessage struct {
	Type        string          `json:"type"`
	DeviceID    string          `json:"device_id,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Text        string          `json:"text,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	State       string          `json:"state,omitempty"`
	Token       string          `json:"token,omitempty"`
	DeviceMac   string          `json:"device_mac,omitempty"`
	Version     int             `json:"version,omitempty"`
	Transport   string          `json:"transport,omitempty"`
	Features    map[string]bool `json:"features,omitempty"`
	AudioParams *AudioFormat    `json:"audio_params,omitempty"`
	PayLoad     json.RawMessage `json:"payload,omitempty"`
}
