package aliyuntts

import "strings"

const (
	CategoryUnknown   = "unknown"
	CategoryQwenTTS   = "qwen_tts"
	CategoryQwenAudio = "qwen_audio"
	CategoryCosyVoice = "aliyun_cosyvoice"
)

type EmotionMode = string

const (
	EmotionNone        EmotionMode = "none"
	EmotionInstruction EmotionMode = "instruction"
	EmotionInlineTag   EmotionMode = "inline_tag"
)

type ModelCapability struct {
	ModelID              string      `json:"model_id"`
	Category             string      `json:"category"`
	SupportsVoiceClone   bool        `json:"supports_voice_clone"`
	SupportsInstruction  bool        `json:"supports_instruction"`
	SupportsLanguageType bool        `json:"supports_language_type"`
	EmotionMode          EmotionMode `json:"emotion_mode"`
	DefaultSampleRate    int         `json:"default_sample_rate"`
}

func GetAliyunModelCapability(model string) ModelCapability {
	model = strings.TrimSpace(model)
	lower := strings.ToLower(model)
	capability := ModelCapability{ModelID: model, Category: CategoryUnknown, EmotionMode: EmotionNone, DefaultSampleRate: 24000}

	switch {
	case strings.HasPrefix(lower, "cosyvoice"):
		capability.Category = CategoryCosyVoice
		capability.SupportsVoiceClone = true
	case strings.HasPrefix(lower, "qwen-audio"):
		capability.Category = CategoryQwenAudio
		capability.SupportsVoiceClone = true
		capability.SupportsInstruction = true
		capability.EmotionMode = EmotionInlineTag
	case strings.HasPrefix(lower, "qwen3-tts"), strings.HasPrefix(lower, "qwen-tts"):
		capability.Category = CategoryQwenTTS
		capability.SupportsVoiceClone = true
		capability.SupportsInstruction = strings.Contains(lower, "instruct")
		capability.SupportsLanguageType = true
		capability.EmotionMode = EmotionInstruction
	}

	return capability
}

// GetModelCapability is kept as a concise alias for callers that do not need the provider prefix.
func GetModelCapability(model string) ModelCapability {
	return GetAliyunModelCapability(model)
}
