package hooks

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	cmap "github.com/orcaman/concurrent-map/v2"

	log "xiaozhi-esp32-server-golang/logger"
)

type turnMetric struct {
	turnID int64

	turnStartTs        int64
	turnEndTs          int64
	voiceSilenceTs     int64
	asrFirstTextTs     int64
	asrFinalTextTs     int64
	llmStartTs         int64
	llmFirstTokenTs    int64
	llmFirstSentenceTs int64
	llmEndTs           int64
	ttsStartTs         int64
	ttsFirstFrameTs    int64
	ttsStopTs          int64
}

type statisticPlugin struct {
	sessions cmap.ConcurrentMap[string, *sessionMetricState]

	cleanupCounter   atomic.Int64
	cleanupThreshold int64
}

type sessionMetricState struct {
	mu sync.Mutex

	nextTurn int64
	current  *turnMetric
	lastSeen int64
	removed  bool
}

func newStatisticPlugin() *statisticPlugin {
	return &statisticPlugin{
		sessions:         cmap.New[*sessionMetricState](),
		cleanupThreshold: 100,
	}
}

func (p *statisticPlugin) Init(context.Context) error { return nil }
func (p *statisticPlugin) Close() error {
	p.sessions.Clear()
	return nil
}

func BuiltinRegistrations() []Registration {
	plugin := newStatisticPlugin()
	meta := PluginMeta{
		Name:        "statistic_plugin",
		Version:     "v1",
		Description: "Track only the latest turn metrics and log on turn end",
		Priority:    100,
		Enabled:     true,
		Kind:        PluginKindInterceptor,
		Stage:       EventChatMetric,
	}
	return []Registration{
		newASRNoiseFilterRegistration(),
		{
			Meta:      meta,
			Lifecycle: plugin,
			Register: func(hub *Hub, meta PluginMeta) error {
				return hub.RegisterInterceptor(EventChatMetric, meta, plugin.onMetricSync)
			},
		},
	}
}

func (p *statisticPlugin) onMetricSync(ctx Context, payload any) (any, bool, error) {
	p.onMetric(ctx, payload)
	return payload, false, nil
}

func (p *statisticPlugin) onMetric(ctx Context, payload any) {
	data, ok := payload.(MetricData)
	if !ok || ctx.SessionID == "" {
		return
	}

	nowTs := time.Now().UnixMilli()
	var completed *turnMetric

	for {
		state := p.getOrCreateSessionState(ctx.SessionID)
		state.mu.Lock()
		if state.removed {
			state.mu.Unlock()
			p.removeSessionState(ctx.SessionID, state)
			continue
		}

		state.lastSeen = nowTs
		tm := state.getTurnForStageLocked(data.Stage)
		if tm != nil {
			completed = state.applyMetricLocked(tm, data)
		}
		state.mu.Unlock()
		break
	}

	if p.cleanupThreshold > 0 && p.cleanupCounter.Add(1)%p.cleanupThreshold == 0 {
		p.cleanupStale(nowTs)
	}

	if completed != nil {
		p.logTurnMetric(ctx.SessionID, completed)
	}
}

func (p *statisticPlugin) getOrCreateSessionState(sessionID string) *sessionMetricState {
	if state, ok := p.sessions.Get(sessionID); ok {
		return state
	}

	state := &sessionMetricState{}
	return p.sessions.Upsert(sessionID, state, func(exist bool, valueInMap, newValue *sessionMetricState) *sessionMetricState {
		if exist {
			return valueInMap
		}
		return newValue
	})
}

func (p *statisticPlugin) removeSessionState(sessionID string, state *sessionMetricState) {
	p.sessions.RemoveCb(sessionID, func(_ string, valueInMap *sessionMetricState, exists bool) bool {
		return exists && valueInMap == state
	})
}

func (s *sessionMetricState) getTurnForStageLocked(stage MetricStage) *turnMetric {
	if stage == MetricTurnStart {
		if s.current != nil && canReuseForLateTurnStart(s.current) {
			return s.current
		}
		return s.startNewTurnLocked()
	}

	if s.current != nil {
		return s.current
	}
	if canStartTurnWithoutTurnStart(stage) {
		return s.startNewTurnLocked()
	}
	return nil
}

func canReuseForLateTurnStart(tm *turnMetric) bool {
	if tm == nil {
		return false
	}
	return tm.turnStartTs == 0 && tm.llmStartTs == 0 && tm.ttsStartTs == 0 && tm.ttsStopTs == 0
}

func canStartTurnWithoutTurnStart(stage MetricStage) bool {
	switch stage {
	case MetricVoiceSilence, MetricAsrFirstText, MetricAsrFinalText, MetricLlmStart, MetricTtsStart:
		return true
	default:
		return false
	}
}

func (s *sessionMetricState) startNewTurnLocked() *turnMetric {
	s.nextTurn++

	tm := &turnMetric{turnID: s.nextTurn}
	s.current = tm
	return tm
}

func (s *sessionMetricState) applyMetricLocked(tm *turnMetric, data MetricData) *turnMetric {
	if data.Stage != MetricTurnStart && tm.turnStartTs > 0 && data.Ts > 0 && data.Ts < tm.turnStartTs {
		return nil
	}

	switch data.Stage {
	case MetricTurnStart:
		if tm.turnStartTs == 0 || (data.Ts > 0 && data.Ts < tm.turnStartTs) {
			tm.turnStartTs = data.Ts
		}
	case MetricTurnEnd:
		if tm.turnEndTs == 0 {
			tm.turnEndTs = data.Ts
		}
		snapshot := *tm
		s.current = nil
		return &snapshot
	case MetricVoiceSilence:
		if tm.voiceSilenceTs == 0 {
			tm.voiceSilenceTs = data.Ts
		}
	case MetricAsrFirstText:
		if tm.asrFirstTextTs == 0 {
			tm.asrFirstTextTs = data.Ts
		}
	case MetricAsrFinalText:
		if tm.asrFinalTextTs == 0 {
			tm.asrFinalTextTs = data.Ts
		}
	case MetricLlmStart:
		if tm.llmStartTs == 0 {
			tm.llmStartTs = data.Ts
		}
	case MetricLlmFirstToken:
		if tm.llmFirstTokenTs == 0 {
			tm.llmFirstTokenTs = data.Ts
		}
	case MetricLlmFirstSentence:
		if tm.llmFirstSentenceTs == 0 {
			tm.llmFirstSentenceTs = data.Ts
		}
	case MetricLlmEnd:
		if tm.llmEndTs == 0 {
			tm.llmEndTs = data.Ts
		}
	case MetricTtsStart:
		if tm.ttsStartTs == 0 {
			tm.ttsStartTs = data.Ts
		}
	case MetricTtsFirstFrame:
		if tm.ttsFirstFrameTs == 0 {
			tm.ttsFirstFrameTs = data.Ts
		}
	case MetricTtsStop:
		if tm.ttsStopTs == 0 && (tm.ttsStartTs > 0 || tm.ttsFirstFrameTs > 0) {
			tm.ttsStopTs = data.Ts
		}
	}
	return nil
}

func calcDelta(start, end int64) int64 {
	if start <= 0 || end <= 0 || end < start {
		return 0
	}
	return end - start
}

func (p *statisticPlugin) logTurnMetric(sessionID string, tm *turnMetric) {
	e2eTotalEndTs := tm.turnEndTs
	if e2eTotalEndTs == 0 {
		e2eTotalEndTs = tm.ttsStopTs
	}

	log.Infof(
		"metric turn=%d session=%s asr_first_text=%dms asr_silence_final=%dms llm_first_token=%dms llm_first_sentence=%dms llm_total=%dms tts_first_token=%dms tts_total=%dms eos_to_tts_first=%dms e2e_first=%dms e2e_total=%dms",
		tm.turnID,
		sessionID,
		calcDelta(tm.turnStartTs, tm.asrFirstTextTs),
		calcDelta(tm.voiceSilenceTs, tm.asrFinalTextTs),
		calcDelta(tm.llmStartTs, tm.llmFirstTokenTs),
		calcDelta(tm.llmStartTs, tm.llmFirstSentenceTs),
		calcDelta(tm.llmStartTs, tm.llmEndTs),
		calcDelta(tm.ttsStartTs, tm.ttsFirstFrameTs),
		calcDelta(tm.ttsStartTs, tm.ttsStopTs),
		calcDelta(tm.voiceSilenceTs, tm.ttsFirstFrameTs),
		calcDelta(tm.turnStartTs, tm.ttsFirstFrameTs),
		calcDelta(tm.turnStartTs, e2eTotalEndTs),
	)
}

func (p *statisticPlugin) cleanupStale(nowTs int64) {
	const ttl = int64(2 * 60 * 1000)

	for item := range p.sessions.IterBuffered() {
		sessionID := item.Key
		state := item.Val

		state.mu.Lock()
		stale := !state.removed && nowTs-state.lastSeen > ttl
		if stale {
			state.removed = true
		}
		state.mu.Unlock()

		if !stale {
			continue
		}
		p.removeSessionState(sessionID, state)
	}
}
