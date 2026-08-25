package aliyuntts

import "testing"

func TestGetModelCapability(t *testing.T) {
	tests := []struct {
		name                string
		model               string
		category            string
		supportsInstruction bool
		supportsLanguage    bool
		emotionMode         EmotionMode
	}{
		{name: "qwen flash", model: "qwen3-tts-flash", category: CategoryQwenTTS, supportsLanguage: true, emotionMode: EmotionInstruction},
		{name: "qwen instruct", model: "qwen3-tts-instruct-flash", category: CategoryQwenTTS, supportsInstruction: true, supportsLanguage: true, emotionMode: EmotionInstruction},
		{name: "qwen audio", model: "qwen-audio-3.0-tts-plus", category: CategoryQwenAudio, supportsInstruction: true, emotionMode: EmotionInlineTag},
		{name: "aliyun cosyvoice", model: "cosyvoice-v3-flash", category: CategoryCosyVoice, emotionMode: EmotionNone},
		{name: "unknown custom model", model: "custom-tts", category: CategoryUnknown, emotionMode: EmotionNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetAliyunModelCapability(tt.model)
			if got.Category != tt.category || got.SupportsInstruction != tt.supportsInstruction || got.SupportsLanguageType != tt.supportsLanguage || got.EmotionMode != tt.emotionMode {
				t.Fatalf("GetModelCapability(%q) = %#v", tt.model, got)
			}
		})
	}
}
