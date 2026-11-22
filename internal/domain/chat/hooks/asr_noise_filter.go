package hooks

import (
	"strings"
	"unicode"

	"github.com/spf13/viper"

	log "xiaozhi-esp32-server-golang/logger"
)

const asrNoiseFilterConfigPath = "chat_hooks.plugins.asr_noise_filter"

var defaultASRNoiseDropPhrases = []string{
	"嗯", "嗯嗯",
	"啊", "啊啊",
	"呃", "额",
	"唔",
	"哦", "噢",
	"标点符号", "标识符号",
	"句号", "逗号",
}

var defaultASRNoiseKeepPhrases = []string{
	"好",
	"对",
	"是",
	"停",
	"开",
	"关",
	"不",
}

type ASRNoiseFilterConfig struct {
	Enabled            bool
	MinVoiceDurationMs int64
	DropPhrases        []string
	KeepPhrases        []string
}

func DefaultASRNoiseFilterConfig() ASRNoiseFilterConfig {
	return ASRNoiseFilterConfig{
		Enabled:            false,
		MinVoiceDurationMs: 300,
		DropPhrases:        cloneStringSlice(defaultASRNoiseDropPhrases),
		KeepPhrases:        cloneStringSlice(defaultASRNoiseKeepPhrases),
	}
}

func LoadASRNoiseFilterConfig() ASRNoiseFilterConfig {
	cfg := DefaultASRNoiseFilterConfig()

	if viper.IsSet(asrNoiseFilterConfigPath + ".enabled") {
		cfg.Enabled = viper.GetBool(asrNoiseFilterConfigPath + ".enabled")
	}
	if viper.IsSet(asrNoiseFilterConfigPath + ".min_voice_duration_ms") {
		cfg.MinVoiceDurationMs = viper.GetInt64(asrNoiseFilterConfigPath + ".min_voice_duration_ms")
	}
	if viper.IsSet(asrNoiseFilterConfigPath + ".drop_phrases") {
		cfg.DropPhrases = viper.GetStringSlice(asrNoiseFilterConfigPath + ".drop_phrases")
	}
	if viper.IsSet(asrNoiseFilterConfigPath + ".keep_phrases") {
		cfg.KeepPhrases = viper.GetStringSlice(asrNoiseFilterConfigPath + ".keep_phrases")
	}

	return cfg
}

func ShouldDropConfiguredASRNoiseText(text string, voiceDurationMs int64) (bool, string) {
	return ShouldDropASRNoiseText(text, voiceDurationMs, LoadASRNoiseFilterConfig())
}

func ShouldDropASRNoiseText(text string, voiceDurationMs int64, cfg ASRNoiseFilterConfig) (bool, string) {
	if !cfg.Enabled {
		return false, "disabled"
	}

	if strings.TrimSpace(text) == "" {
		return false, "empty"
	}

	normalized := normalizeASRNoiseText(text)
	if normalized == "" {
		return true, "punctuation_only"
	}

	if containsNormalizedASRPhrase(cfg.KeepPhrases, normalized) {
		return false, "keep_phrase"
	}

	if containsNormalizedASRPhrase(cfg.DropPhrases, normalized) {
		return true, "drop_phrase"
	}

	if cfg.MinVoiceDurationMs > 0 &&
		voiceDurationMs > 0 &&
		voiceDurationMs < cfg.MinVoiceDurationMs &&
		isLikelyShortFiller(normalized) {
		return true, "short_filler"
	}

	return false, "not_noise"
}

func newASRNoiseFilterRegistration() Registration {
	meta := PluginMeta{
		Name:        "asr_noise_filter",
		Version:     "v1",
		Description: "Drop short ASR filler or punctuation-only results before STT/LLM",
		Priority:    10,
		Enabled:     false,
		Kind:        PluginKindInterceptor,
		Stage:       EventChatASROutput,
	}
	return Registration{
		Meta: meta,
		Register: func(hub *Hub, meta PluginMeta) error {
			return hub.RegisterInterceptor(EventChatASROutput, meta, func(ctx Context, payload any) (any, bool, error) {
				data, ok := payload.(ASROutputData)
				if !ok {
					return payload, false, nil
				}

				drop, reason := ShouldDropConfiguredASRNoiseText(data.Text, data.VoiceDurationMs)
				if !drop {
					return payload, false, nil
				}

				log.Infof(
					"ASR噪声过滤命中，跳过STT/LLM: device=%s, session=%s, reason=%s, voice_duration_ms=%d, text=%q",
					ctx.DeviceID,
					ctx.SessionID,
					reason,
					data.VoiceDurationMs,
					data.Text,
				)
				return data, true, nil
			})
		},
	}
}

func normalizeASRNoiseText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func containsNormalizedASRPhrase(phrases []string, normalizedText string) bool {
	for _, phrase := range phrases {
		if normalizeASRNoiseText(phrase) == normalizedText {
			return true
		}
	}
	return false
}

func isLikelyShortFiller(normalized string) bool {
	runes := []rune(normalized)
	if len(runes) == 0 || len(runes) > 2 {
		return false
	}
	for _, r := range runes {
		switch r {
		case '嗯', '啊', '呃', '额', '唔', '哦', '噢', '哼':
		default:
			return false
		}
	}
	return true
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}
