package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"sync"

	utypes "xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	llm_common "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/memory"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
	"xiaozhi-esp32-server-golang/internal/domain/tts"

	. "xiaozhi-esp32-server-golang/internal/data/audio"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"
)

// Dialogue 表示对话历史
type Dialogue struct {
	mu       sync.RWMutex // 保护 Messages 的读写锁
	Messages []*schema.Message
}

const (
	ClientStatusInit       = "init"
	ClientStatusListening  = "listening"
	ClientStatusListenStop = "listenStop"
	ClientStatusLLMStart   = "llmStart"
	ClientStatusTTSStart   = "ttsStart"

	ListenPhaseIdle      = "idle"
	ListenPhaseStarting  = "starting"
	ListenPhaseListening = "listening"

	CommandTypeDetect      = "detect"
	CommandTypeListenStart = "listen_start"
	CommandTypeListenStop  = "listen_stop"

	MemoryModeNone  = "none"
	MemoryModeShort = "short"
	MemoryModeLong  = "long"

	SpeakerChatModeOff            = "off"
	SpeakerChatModeIdentifiedOnly = "identified_only"
)

func NormalizeMemoryMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case MemoryModeNone:
		return MemoryModeNone
	case MemoryModeLong:
		return MemoryModeLong
	default:
		return MemoryModeShort
	}
}

func NormalizeSpeakerChatMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SpeakerChatModeIdentifiedOnly:
		return SpeakerChatModeIdentifiedOnly
	default:
		return SpeakerChatModeOff
	}
}

type SendAudioData func(audioData []byte) error

// ClientState 表示客户端状态
type ClientState struct {
	cmdMu sync.Mutex

	IsActivated bool
	// 对话历史
	Dialogue *Dialogue
	// 打断状态
	Abort bool
	// 拾音模式
	ListenMode string
	// listen start 流程状态: idle / starting / listening
	ListenPhase string
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
	TTSProvider      tts.TTSProvider        // 默认TTS提供者
	SpeakerTTSConfig map[string]interface{} // 声纹识别的TTS配置（完整config，优先使用）
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
	OpusAudioBuffer chan []byte

	// pcm接收的音频数据缓冲区
	AsrAudioBuffer *AsrAudioBuffer

	VoiceStatus
	AudioIdle AudioIdleClock

	UdpSendAudioData SendAudioData //发送音频数据
	Statistic        Statistic     //耗时统计
	MqttLastActiveTs int64         //最后活跃时间
	VadLastActiveTs  int64         //vad最后活跃时间, 超过 60s && 没有在tts则断开连接

	Status string //状态 listening, llmStart, ttsStart

	IsTtsStart        bool //是否tts开始
	IsWelcomeSpeaking bool //是否已经播放过欢迎语
	IsWelcomePlaying  bool //是否正在播放欢迎语

	LastCmdType string
	LastCmdAt   time.Time

	// 声纹识别相关
	SpeakerProvider speaker.SpeakerProvider // 声纹识别提供者（在 session 中初始化）

	// 异步获取声纹结果的回调函数（在 session 中设置）
	OnVoiceSilenceSpeakerCallback func(ctx context.Context)

	// 语音静默事件指标回调函数（在 session 中设置）
	OnVoiceSilenceMetricCallback func(ctx context.Context, ts int64)

	// ASR首次返回字符的回调函数（在 session 中设置）
	OnAsrFirstTextCallback func(text string, isFinal bool)
}

// IsSpeakerEnabled 检查是否启用声纹识别（从全局配置中读取）
func (c *ClientState) IsSpeakerEnabled() bool {
	// 从全局配置（viper）获取 enable 字段
	enabled := viper.GetBool("voice_identify.enable")
	return enabled
}

// HasSpeakerGroups 检查设备配置中是否有声纹组
func (c *ClientState) HasSpeakerGroups() bool {
	// 检查设备配置中是否有声纹组配置
	return len(c.DeviceConfig.VoiceIdentify) > 0
}

func (c *ClientState) IsRealTime() bool {
	return c.ListenMode == "realtime"
}

func (c *ClientState) GetMemoryMode() string {
	return NormalizeMemoryMode(c.DeviceConfig.MemoryMode)
}

func (c *ClientState) GetSpeakerChatMode() string {
	return NormalizeSpeakerChatMode(c.DeviceConfig.SpeakerChatMode)
}

func (c *ClientState) RequireMatchedSpeakerForChat() bool {
	return c.HasSpeakerGroups() && c.GetSpeakerChatMode() == SpeakerChatModeIdentifiedOnly
}

func (c *ClientState) HasMatchedConfiguredSpeaker(result *speaker.IdentifyResult) bool {
	if result == nil || !result.Identified {
		return false
	}
	_, ok := c.DeviceConfig.VoiceIdentify[result.SpeakerName]
	return ok
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
	c.Dialogue.mu.Lock()
	defer c.Dialogue.mu.Unlock()
	c.Dialogue.Messages = append(c.Dialogue.Messages, msg)
}

func (c *ClientState) GetMessages(count int) []*schema.Message {
	c.Dialogue.mu.RLock()
	defer c.Dialogue.mu.RUnlock()

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

func (c *ClientState) InitMessages(messages []*schema.Message) error {
	c.Dialogue.mu.Lock()
	defer c.Dialogue.mu.Unlock()
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
	if !viper.IsSet("chat.max_idle_duration") {
		return 30000
	}

	maxIdleDuration := viper.GetInt64("chat.max_idle_duration")
	if maxIdleDuration <= 0 {
		return math.MaxInt64
	}
	return maxIdleDuration
}

func (c *ClientState) UsesAudioIdleClock() bool {
	if c == nil {
		return false
	}
	return c.ListenMode == "auto" || c.IsRealTime()
}

func (c *ClientState) ShouldCountAudioIdleTimeout() bool {
	if c == nil || !c.IsRealTime() {
		return true
	}
	if c.GetTtsStart() {
		return false
	}
	switch c.GetStatus() {
	case ClientStatusLLMStart, ClientStatusTTSStart:
		return false
	default:
		return true
	}
}

func (c *ClientState) StartAudioIdleWindow(now time.Time) {
	if c == nil || !c.UsesAudioIdleClock() {
		return
	}
	c.AudioIdle.Start(now)
	c.SetClientVoiceStop(false)
}

func (c *ClientState) PauseAudioIdleWindow(now time.Time) {
	if c == nil || !c.UsesAudioIdleClock() {
		return
	}
	c.AudioIdle.Pause(now)
}

func (c *ClientState) ResumeAudioIdleWindow(now time.Time) {
	if c == nil || !c.UsesAudioIdleClock() {
		return
	}
	c.AudioIdle.Resume(now)
	c.SetClientVoiceStop(false)
}

func (c *ClientState) ResetAudioIdleWindow() {
	if c == nil {
		return
	}
	c.AudioIdle.Reset()
}

func (c *ClientState) GetAudioIdleElapsed(now time.Time) time.Duration {
	if c == nil {
		return 0
	}
	return c.AudioIdle.Elapsed(now)
}

func (c *ClientState) AudioIdleStarted() bool {
	if c == nil {
		return false
	}
	return c.AudioIdle.Started()
}

func (c *ClientState) AudioIdlePaused() bool {
	if c == nil {
		return false
	}
	return c.AudioIdle.Paused()
}

func (c *ClientState) MarkAudioIdleTimeoutPending() bool {
	if c == nil {
		return false
	}
	return c.AudioIdle.MarkTimeoutPending()
}

func (c *ClientState) ClearAudioIdleTimeoutPending() {
	if c == nil {
		return
	}
	c.AudioIdle.ClearTimeoutPending()
}

func (c *ClientState) AudioIdleTimeoutPending() bool {
	if c == nil {
		return false
	}
	return c.AudioIdle.TimeoutPending()
}

func (c *ClientState) GetPreAsrTextSilenceDuration() int64 {
	if viper.IsSet("chat.pre_asr_text_silence_duration") {
		preTextSilenceDuration := viper.GetInt64("chat.pre_asr_text_silence_duration")
		if preTextSilenceDuration <= 0 {
			return math.MaxInt64
		}
		return preTextSilenceDuration
	}

	base := c.VoiceStatus.SilenceThresholdTime
	if base <= 0 {
		base = 400
	}
	preTextSilenceDuration := base * 4
	if preTextSilenceDuration < 1000 {
		preTextSilenceDuration = 1000
	}
	return preTextSilenceDuration
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

func (c *ClientState) SetListenPhase(phase string) {
	c.ListenPhase = phase
}

func (c *ClientState) GetListenPhase() string {
	return c.ListenPhase
}

type Ctx struct {
	sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *Ctx) Reset() {
	c.ResetWithReason("Ctx.Reset")
}

func (c *Ctx) ResetWithReason(reason string) {
	c.Lock()
	defer c.Unlock()
	if c.ctx != nil {
		log.Debugf("Ctx.ResetWithReason: reason=%s", reason)
		c.cancel()
		c.ctx = nil
		c.cancel = nil
	}
}

func (c *Ctx) Get(parentCtx context.Context) context.Context {
	c.Lock()
	defer c.Unlock()
	if c.ctx == nil || c.ctx.Err() != nil {
		if c.ctx != nil {
			c.cancel()
		}
		c.ctx, c.cancel = context.WithCancel(parentCtx)
	}
	return c.ctx
}

func (c *Ctx) Cancel() {
	c.CancelWithReason("Ctx.Cancel")
}

func (c *Ctx) CancelWithReason(reason string) {
	c.Lock()
	defer c.Unlock()
	if c.ctx != nil {
		log.Debugf("Ctx.CancelWithReason: reason=%s", reason)
		c.cancel()
		c.ctx = nil
		c.cancel = nil
	}
}

func (s *ClientState) getLLMProvider() (llm.LLMProvider, error) {
	llmConfig := s.DeviceConfig.Llm
	providerName := llmConfig.Provider
	if providerName == "" {
		providerName = "openai"
	}
	llmProvider, err := llm.GetLLMProvider(providerName, llmConfig.Config)
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

	//初始化asr（不再直接创建 AsrProvider，改为使用资源池）
	ctx, cancel := context.WithCancel(s.Ctx)
	s.Asr = Asr{
		Ctx:             ctx,
		Cancel:          cancel,
		AsrAudioChannel: make(chan []float32, 100),
		AsrEnd:          make(chan bool, 1),
		AsrResult:       bytes.Buffer{},
		AsrType:         asrConfig.Provider,
		ClientState:     s, // 设置 ClientState 引用
	}

	// 设置 ASR 模式
	if mode, ok := asrConfig.Config["mode"].(string); ok {
		s.Asr.Mode = mode
	}

	if rawAutoEnd, ok := asrConfig.Config["auto_end"]; ok {
		if autoEnd, ok := rawAutoEnd.(bool); ok {
			s.Asr.AutoEnd = autoEnd
		}
	}
	return nil
}

func (c *ClientState) Destroy() {
	c.Asr.StopWithReason("ClientState.Destroy")
	c.Vad.Reset()
	c.ResetAudioIdleWindow()
	c.ClearAudioIdleTimeoutPending()

	// 归还ASR资源（如果存在）
	// 注意：这里需要导入 pool 包，但为了避免循环依赖，在调用处处理
	// 或者在这里使用类型断言，但需要导入 pool 包
	// 暂时在调用处（ChatSession.Close）处理资源归还

	c.VoiceStatus.Reset()
	c.AsrAudioBuffer.ClearAsrAudioData()

	c.SessionCtx.ResetWithReason("ClientState.Destroy: session_ctx")
	c.AfterAsrSessionCtx.ResetWithReason("ClientState.Destroy: after_asr_ctx")

	c.Statistic.Reset()
	c.SetStatus(ClientStatusInit)
	c.SetListenPhase(ListenPhaseIdle)
	c.SetTtsStart(false)
}

type CommandHistorySnapshot struct {
	LastCmdType string
	LastCmdAt   time.Time
}

func (s CommandHistorySnapshot) DebugString(now time.Time) string {
	formatAt := func(at time.Time) string {
		if at.IsZero() {
			return "zero"
		}
		return at.Format(time.RFC3339Nano)
	}
	formatAge := func(at time.Time) string {
		if at.IsZero() {
			return "n/a"
		}
		return now.Sub(at).Truncate(time.Millisecond).String()
	}

	return fmt.Sprintf(
		"lastCmd=%q lastCmdAt=%s lastCmdAge=%s",
		s.LastCmdType,
		formatAt(s.LastCmdAt),
		formatAge(s.LastCmdAt),
	)
}

func (c *ClientState) RecordCommandArrival(cmdType string, at time.Time) {
	c.cmdMu.Lock()
	c.LastCmdType = cmdType
	c.LastCmdAt = at
	c.cmdMu.Unlock()
}

func (c *ClientState) GetCommandHistorySnapshot() CommandHistorySnapshot {
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()
	return CommandHistorySnapshot{
		LastCmdType: c.LastCmdType,
		LastCmdAt:   c.LastCmdAt,
	}
}

func (state *ClientState) OnManualStop() {
	state.ClearAudioIdleTimeoutPending()
	state.OnVoiceSilence()
}

func (state *ClientState) OnVoiceSilence() {
	silenceTs := time.Now().UnixMilli()
	log.Debugf("OnVoiceSilence, voiceDuration: %d, voiceDurationInSession: %d", state.Vad.GetVoiceDuration(), state.Vad.GetVoiceDurationInSession())
	if state.MarkVoiceSilenceAt(silenceTs) && state.OnVoiceSilenceMetricCallback != nil {
		state.OnVoiceSilenceMetricCallback(state.Ctx, silenceTs)
	}
	state.Asr.ResetReceivedText()
	state.SetClientVoiceStop(true) //设置停止说话标志位, 此时收到的音频数据不会进vad
	//客户端停止说话
	state.Asr.StopWithReason("ClientState.OnVoiceSilence") //停止asr并获取结果，进行llm
	//释放vad
	state.Vad.Reset() //释放vad实例

	state.SetStatus(ClientStatusListenStop)
	state.SetListenPhase(ListenPhaseIdle)

	// 如果设置了异步获取声纹结果的回调，则调用
	if state.OnVoiceSilenceSpeakerCallback != nil {
		state.OnVoiceSilenceSpeakerCallback(state.Ctx)
	}
}

type Llm struct {
	Ctx    context.Context
	Cancel context.CancelFunc
	// LLM 提供者
	LLMProvider llm.LLMProvider
	//asr to text接收的通道
	LLmRecvChannel chan llm_common.LLMResponseStruct
}

type SpeakReadyUDPConfig struct {
	Ready         bool `json:"ready"`
	ReuseExisting bool `json:"reuse_existing,omitempty"`
}

// ClientMessage 表示客户端消息
type ClientMessage struct {
	Type           string               `json:"type"`
	DeviceID       string               `json:"device_id,omitempty"`
	SessionID      string               `json:"session_id,omitempty"`
	Text           string               `json:"text,omitempty"`
	Mode           string               `json:"mode,omitempty"`
	State          string               `json:"state,omitempty"`
	Token          string               `json:"token,omitempty"`
	DeviceMac      string               `json:"device_mac,omitempty"`
	Version        int                  `json:"version,omitempty"`
	Transport      string               `json:"transport,omitempty"`
	Features       map[string]bool      `json:"features,omitempty"`
	AudioParams    *AudioFormat         `json:"audio_params,omitempty"`
	SpeakUDPConfig *SpeakReadyUDPConfig `json:"udp_config,omitempty"`
	PayLoad        json.RawMessage      `json:"payload,omitempty"`
}
