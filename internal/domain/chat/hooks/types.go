package hooks

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
)

type Context struct {
	Ctx       context.Context
	SessionID string
	DeviceID  string
}

type PluginKind string

type EventKind string

const (
	PluginKindInterceptor PluginKind = "interceptor"
	PluginKindObserver    PluginKind = "observer"
)

const (
	EventKindInterceptor EventKind = "interceptor"
	EventKindObserver    EventKind = "observer"
)

type PluginMeta struct {
	Name        string
	Version     string
	Description string
	Priority    int
	Enabled     bool
	Kind        PluginKind
	Stage       string
}

type Lifecycle interface {
	Init(context.Context) error
	Close() error
}

type Registration struct {
	Meta      PluginMeta
	Register  func(*Hub, PluginMeta) error
	Lifecycle Lifecycle
}

type Registry interface {
	Add(Registration)
	List() []Registration
}

type InMemoryRegistry struct {
	regs []Registration
}

func NewRegistry() *InMemoryRegistry { return &InMemoryRegistry{} }

func (r *InMemoryRegistry) Add(reg Registration) {
	if r == nil {
		return
	}
	r.regs = append(r.regs, reg)
}

func (r *InMemoryRegistry) List() []Registration {
	if r == nil {
		return nil
	}
	out := make([]Registration, len(r.regs))
	copy(out, r.regs)
	return out
}

type BuiltinPluginConfig struct {
	Enabled  *bool
	Priority int
}

const (
	EventChatASROutput      = "chat.asr.output"
	EventChatLLMInput       = "chat.llm.input"
	EventChatLLMOutputRaw   = "chat.llm.output.raw"
	EventChatTTSInput       = "chat.tts.input"
	EventChatTTSOutputStart = "chat.tts.output.start"
	EventChatTTSOutputStop  = "chat.tts.output.stop"
	EventChatMetric         = "chat.metric"
)

var eventKinds = map[string]EventKind{
	EventChatASROutput:      EventKindInterceptor,
	EventChatLLMInput:       EventKindInterceptor,
	EventChatLLMOutputRaw:   EventKindInterceptor,
	EventChatTTSInput:       EventKindInterceptor,
	EventChatTTSOutputStart: EventKindObserver,
	EventChatTTSOutputStop:  EventKindObserver,
	EventChatMetric:         EventKindObserver,
}

func ValidateEventKind(event string, kind PluginKind) error {
	expected, ok := eventKinds[event]
	if !ok {
		return fmt.Errorf("unknown hook event: %s", event)
	}
	if event == EventChatMetric && kind == PluginKindInterceptor {
		return nil
	}
	if PluginKind(expected) != kind {
		return fmt.Errorf("hook event %s requires %s registration, got %s", event, expected, kind)
	}
	return nil
}

type ASROutputData struct {
	Text            string
	SpeakerResult   *speaker.IdentifyResult
	VoiceDurationMs int64
}

type LLMInputData struct {
	UserMessage     *schema.Message
	RequestMessages []*schema.Message
	Tools           []*schema.ToolInfo
}

type LLMOutputRawData struct {
	Delta     string
	FullText  string
	ToolCalls []schema.ToolCall
	IsEnd     bool
	Err       error
}

type TTSInputData struct {
	Text    string
	IsStart bool
	IsEnd   bool
}

type TTSOutputStartData struct{}

type TTSOutputStopData struct {
	Err error
}

type MetricStage string

const (
	MetricTurnStart        MetricStage = "turn_start"
	MetricTurnEnd          MetricStage = "turn_end"
	MetricVoiceSilence     MetricStage = "voice_silence"
	MetricAsrFirstText     MetricStage = "asr_first_text"
	MetricAsrFinalText     MetricStage = "asr_final_text"
	MetricLlmStart         MetricStage = "llm_start"
	MetricLlmFirstToken    MetricStage = "llm_first_token"
	MetricLlmFirstSentence MetricStage = "llm_first_sentence"
	MetricLlmEnd           MetricStage = "llm_end"
	MetricTtsStart         MetricStage = "tts_start"
	MetricTtsFirstFrame    MetricStage = "tts_first_frame"
	MetricTtsStop          MetricStage = "tts_stop"
)

type MetricData struct {
	Stage MetricStage
	Ts    int64
	Err   error
}
