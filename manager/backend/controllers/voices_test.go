package controllers

import (
	"testing"
)

func TestGetVoicesByModel(t *testing.T) {
	tests := []struct {
		model            string
		expectedMinCount int
		containsVoice    string
	}{
		{
			model:            "qwen3-tts-flash",
			expectedMinCount: 5,
			containsVoice:    "Cherry",
		},
		{
			model:            "cosyvoice-v3-flash",
			expectedMinCount: 5,
			containsVoice:    "longxiaochun",
		},
		{
			model:            "cosyvoice-v2",
			expectedMinCount: 5,
			containsVoice:    "cosyvoice-v2-longmiao",
		},
		{
			model:            "qwen-audio-3.0-tts-flash",
			expectedMinCount: 1026,
			containsVoice:    "qwen-audio-3.0-tts-flash-longcanzhuyue",
		},
		{
			model:            "qwen-audio-3.0-tts-plus",
			expectedMinCount: 1026,
			containsVoice:    "qwen-audio-3.0-tts-plus-loongknoxli",
		},
		{
			model:            "qwen-tts-latest",
			expectedMinCount: 5,
			containsVoice:    "Cherry",
		},
	}

	for _, tt := range tests {
		voices := GetVoicesByModel(tt.model)
		if len(voices) < tt.expectedMinCount {
			t.Errorf("GetVoicesByModel(%q) returned %d voices, expected at least %d", tt.model, len(voices), tt.expectedMinCount)
		}
		found := false
		for _, v := range voices {
			if v.Value == tt.containsVoice {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetVoicesByModel(%q) should contain voice %q, but not found", tt.model, tt.containsVoice)
		}
	}
}

func TestQwenAudioSystemVoicesAreModelSpecific(t *testing.T) {
	flash := GetVoicesByModel("qwen-audio-3.0-tts-flash")
	plus := GetVoicesByModel("qwen-audio-3.0-tts-plus")
	if len(flash) != 1026 || len(plus) != 1026 {
		t.Fatalf("Qwen Audio voice counts = flash %d, plus %d; want 1026 each", len(flash), len(plus))
	}
	if IsVoiceSupported("qwen-audio-3.0-tts-flash", plus[0].Value) {
		t.Fatalf("Flash model unexpectedly accepts Plus voice %q", plus[0].Value)
	}
	if IsVoiceSupported("qwen-audio-3.0-tts-plus", flash[0].Value) {
		t.Fatalf("Plus model unexpectedly accepts Flash voice %q", flash[0].Value)
	}
}

func TestQwenAudioVoiceOptionsExposeSpreadsheetVoices(t *testing.T) {
	options := GetAliyunQwenVoicesByModel("qwen-audio-3.0-tts-flash")
	if len(options) != 1026 {
		t.Fatalf("Qwen Audio Flash options = %d, want 1026", len(options))
	}
	if options[0].Value != "qwen-audio-3.0-tts-flash-longcanzhuyue" || options[0].Label != "龙璨竹月" {
		t.Fatalf("first Qwen Audio Flash option = %#v", options[0])
	}
}

func TestLegacyQwenTTSDoesNotExposeQwen3OnlyVoices(t *testing.T) {
	voices := GetVoicesByModel("qwen-tts-2025-05-22")
	for _, voice := range voices {
		if voice.Value == "Vivian" {
			t.Fatal("legacy qwen-tts unexpectedly exposes Qwen3-only voice Vivian")
		}
	}
}

func TestUnknownAliyunModelDoesNotMasqueradeAsQwen3(t *testing.T) {
	if voices := GetAliyunQwenVoicesByModel("custom-unknown-model"); len(voices) != 0 {
		t.Fatalf("unknown model returned %d system voices, want 0", len(voices))
	}
}
