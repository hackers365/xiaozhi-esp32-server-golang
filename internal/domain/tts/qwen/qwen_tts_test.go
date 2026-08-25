package qwen

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/domain/emotion"
)

func TestNewQwenTTSProviderDefaultsAndSetVoice(t *testing.T) {
	provider := NewQwenTTSProvider(map[string]interface{}{})

	if provider.APIURL != defaultAPIURLBeijing {
		t.Fatalf("APIURL = %q", provider.APIURL)
	}
	if provider.Model != defaultQwenModel {
		t.Fatalf("Model = %q", provider.Model)
	}
	if provider.Voice != defaultQwenVoice {
		t.Fatalf("Voice = %q", provider.Voice)
	}
	if provider.LanguageType != defaultQwenLanguageType {
		t.Fatalf("LanguageType = %q", provider.LanguageType)
	}
	if provider.FrameDuration != audio.FrameDuration {
		t.Fatalf("FrameDuration = %d", provider.FrameDuration)
	}
	if !provider.IsValid() {
		t.Fatal("provider should be valid")
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}

	if err := provider.SetVoice(map[string]interface{}{"voice": "Chelsie"}); err != nil {
		t.Fatalf("SetVoice error = %v", err)
	}
	if provider.Voice != "Chelsie" {
		t.Fatalf("Voice = %q", provider.Voice)
	}
	if err := provider.SetVoice(map[string]interface{}{}); err == nil {
		t.Fatal("expected missing voice to fail")
	}
}

func TestNewQwenTTSProviderSingaporeRegion(t *testing.T) {
	provider := NewQwenTTSProvider(map[string]interface{}{"region": "singapore"})
	if provider.APIURL != defaultAPIURLSingapore {
		t.Fatalf("APIURL = %q", provider.APIURL)
	}
}

func TestNewQwenTTSProviderDefaultsLanguageOnlyForQwenTTS(t *testing.T) {
	if got := NewQwenTTSProvider(map[string]interface{}{"model": "qwen3-tts-flash"}).LanguageType; got != "Chinese" {
		t.Fatalf("Qwen LanguageType = %q", got)
	}
	if got := NewQwenTTSProvider(map[string]interface{}{"model": "cosyvoice-v3-flash"}).LanguageType; got != "" {
		t.Fatalf("CosyVoice LanguageType = %q, want empty", got)
	}
	if got := NewQwenTTSProvider(map[string]interface{}{"model": "custom-tts"}).LanguageType; got != "" {
		t.Fatalf("custom LanguageType = %q, want empty", got)
	}
}

func TestNewQwenTTSProviderFormat(t *testing.T) {
	provider1 := NewQwenTTSProvider(map[string]interface{}{"stream": false})
	if provider1.Format != "wav" {
		t.Fatalf("expected format 'wav', got %q", provider1.Format)
	}

	provider2 := NewQwenTTSProvider(map[string]interface{}{"stream": true})
	if provider2.Format != "pcm" {
		t.Fatalf("expected format 'pcm', got %q", provider2.Format)
	}

	provider3 := NewQwenTTSProvider(map[string]interface{}{"format": "opus"})
	if provider3.Format != "opus" {
		t.Fatalf("expected format 'opus', got %q", provider3.Format)
	}
}

func TestCleanBase64RemovesWhitespace(t *testing.T) {
	got := cleanBase64(" YWJj\nZGU=\t")
	if got != "YWJjZGU=" {
		t.Fatalf("cleanBase64 = %q", got)
	}
}

func TestQwenRequestParamsForOpusOnlyOnStreamingFlashModel(t *testing.T) {
	params := qwenRequestParamsFor("qwen3-tts-flash", "opus", true)
	if params == nil || params.ResponseFormat != "opus" {
		t.Fatalf("qwen3 flash opus params = %#v", params)
	}

	if params := qwenRequestParamsFor("qwen3-tts-instruct-flash", "opus", true); params != nil {
		t.Fatalf("instruct model should not send opus response_format: %#v", params)
	}

	if params := qwenRequestParamsFor("qwen3-tts-flash", "opus", false); params != nil {
		t.Fatalf("non-streaming should not send opus response_format: %#v", params)
	}
}

func TestQwenStreamDecoderFormatMatchesRequestedAudio(t *testing.T) {
	if got := qwenStreamDecoderFormat("qwen3-tts-flash", "opus"); got != "opus" {
		t.Fatalf("flash opus decoder format = %q", got)
	}
	if got := qwenStreamDecoderFormat("qwen-audio-3.0-tts-flash", "ogg_opus"); got != "ogg_opus" {
		t.Fatalf("qwen audio ogg opus decoder format = %q", got)
	}
	if got := qwenStreamDecoderFormat("cosyvoice-v3-flash", "ogg_opus"); got != "ogg_opus" {
		t.Fatalf("cosyvoice ogg opus decoder format = %q", got)
	}
	if got := qwenStreamDecoderFormat("qwen3-tts-instruct-flash", "opus"); got != "opus" {
		t.Fatalf("instruct opus decoder format = %q", got)
	}
	if got := qwenStreamDecoderFormat("qwen3-tts-flash", ""); got != "pcm" {
		t.Fatalf("empty decoder format = %q", got)
	}
}

func TestQwenAPIAudioFormatMapsOggOpus(t *testing.T) {
	if got := qwenAPIAudioFormat("ogg_opus"); got != "opus" {
		t.Fatalf("qwenAPIAudioFormat(ogg_opus) = %q", got)
	}
	if got := qwenAPIAudioFormat("opus"); got != "opus" {
		t.Fatalf("qwenAPIAudioFormat(opus) = %q", got)
	}
}

func TestPrepareQwenHTTPRequestUsesSpeechSynthesizerFieldsForQwenAudio(t *testing.T) {
	input := qwenRequestInput{
		Text:                 "hello",
		Voice:                "qwen-audio-3.0-tts-flash-demo",
		LanguageType:         "Chinese",
		Instructions:         "warm voice",
		OptimizeInstructions: true,
	}
	params, err := prepareQwenHTTPRequest("qwen-audio-3.0-tts-flash", "ogg_opus", false, &input)
	if err != nil {
		t.Fatal(err)
	}
	if params != nil {
		t.Fatalf("parameters = %#v, want nil", params)
	}
	if input.Instruction != "warm voice" || input.Instructions != "" || input.OptimizeInstructions {
		t.Fatalf("instruction fields = %#v", input)
	}
	if input.LanguageType != "" || input.Format != "opus" || input.SampleRate != 24000 {
		t.Fatalf("SpeechSynthesizer input = %#v", input)
	}
}

func TestPrepareQwenHTTPRequestKeepsQwenTTSProtocol(t *testing.T) {
	input := qwenRequestInput{Text: "hello", Voice: "Cherry", LanguageType: "Chinese", Instructions: "warm voice", OptimizeInstructions: true}
	params, err := prepareQwenHTTPRequest("qwen3-tts-instruct-flash", "wav", false, &input)
	if err != nil {
		t.Fatal(err)
	}
	if params != nil {
		t.Fatalf("parameters = %#v, want nil", params)
	}
	if input.Instructions != "warm voice" || input.Instruction != "" || input.Format != "" || input.SampleRate != 0 {
		t.Fatalf("Qwen-TTS input changed protocol: %#v", input)
	}
}

func TestBuildQwenInstructionUsesDetailedVoiceDirections(t *testing.T) {
	tests := []struct {
		name     string
		emotion  emotion.Emotion
		contains []string
	}{
		{
			name:     "angry",
			emotion:  emotion.EmotionAngry,
			contains: []string{"明显带怒意", "压迫感", "不要破音或失真"},
		},
		{
			name:     "sad",
			emotion:  emotion.EmotionSad,
			contains: []string{"明显哭腔", "轻微哽咽", "不要尖叫或失真"},
		},
		{
			name:     "warm",
			emotion:  emotion.EmotionWarm,
			contains: []string{"语速稍慢", "自然停顿", "安静陪伴"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instruction := buildQwenInstruction(emotion.GetEmotionConfig(tt.emotion))
			for _, want := range tt.contains {
				if !strings.Contains(instruction, want) {
					t.Fatalf("instruction %q does not contain %q", instruction, want)
				}
			}
		})
	}

	if instruction := buildQwenInstruction(emotion.GetEmotionConfig(emotion.EmotionNeutral)); instruction != "" {
		t.Fatalf("neutral instruction = %q, want empty", instruction)
	}
}

func TestApplyQwenExpressionUsesModelCapabilities(t *testing.T) {
	happy := emotion.GetEmotionConfig(emotion.EmotionHappy)
	neutral := emotion.GetEmotionConfig(emotion.EmotionNeutral)

	tests := []struct {
		name             string
		model            string
		voicePrompt      string
		emotion          *emotion.EmotionConfig
		wantModel        string
		wantInstructions string
		wantText         string
	}{
		{name: "qwen flash upgrades for emotion", model: "qwen3-tts-flash", emotion: &happy, wantModel: "qwen3-tts-instruct-flash", wantInstructions: buildQwenInstruction(happy), wantText: "hello"},
		{name: "neutral emotion keeps qwen flash", model: "qwen3-tts-flash", emotion: &neutral, wantModel: "qwen3-tts-flash", wantText: "hello"},
		{name: "qwen instruct uses explicit prompt", model: "qwen3-tts-instruct-flash", voicePrompt: "warm voice", wantModel: "qwen3-tts-instruct-flash", wantInstructions: "warm voice", wantText: "hello"},
		{name: "qwen audio uses official inline emotion", model: "qwen-audio-3.0-tts-flash", emotion: &happy, wantModel: "qwen-audio-3.0-tts-flash", wantText: "[excited]hello"},
		{name: "qwen audio combines instruction and inline emotion", model: "qwen-audio-3.0-tts-flash", voicePrompt: "warm voice", emotion: &happy, wantModel: "qwen-audio-3.0-tts-flash", wantInstructions: "warm voice", wantText: "[excited]hello"},
		{name: "aliyun cosyvoice ignores qwen audio emotion tags", model: "cosyvoice-v3-flash", voicePrompt: "warm voice", emotion: &happy, wantModel: "cosyvoice-v3-flash", wantText: "hello"},
		{name: "unknown model stays conservative", model: "custom-tts", voicePrompt: "warm voice", emotion: &happy, wantModel: "custom-tts", wantText: "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := qwenRequestInput{Text: "hello", Voice: "Cherry"}
			gotModel := applyQwenExpression(tt.model, &input, tt.voicePrompt, tt.emotion)
			if gotModel != tt.wantModel || input.Instructions != tt.wantInstructions || input.Text != tt.wantText {
				t.Fatalf("model=%q input=%#v", gotModel, input)
			}
		})
	}
}

func TestQwenInlineEmotionTagUsesOfficialQwenAudioTags(t *testing.T) {
	tests := []struct {
		emotion emotion.Emotion
		want    string
	}{
		{emotion.EmotionHappy, "[excited]"},
		{emotion.EmotionComfort, "[empathetic]"},
		{emotion.EmotionSerious, "[serious]"},
		{emotion.EmotionCurious, "[curious]"},
		{emotion.EmotionAmazed, "[amazed]"},
		{emotion.EmotionDeepShout, "[deep and loud shouting]"},
		{emotion.EmotionTrembling, "[trembling]"},
		{emotion.EmotionSarcastic, "[sarcastic]"},
		{emotion.EmotionDracula, "[like dracula]"},
		{emotion.EmotionBored, "[bored]"},
		{emotion.EmotionTired, "[tired]"},
		{emotion.EmotionScornful, "[scornful]"},
		{emotion.EmotionShouting, "[shouting]"},
		{emotion.EmotionASMR, "[asmr]"},
		{emotion.EmotionPanicked, "[panicked]"},
		{emotion.EmotionMischievous, "[mischievously]"},
		{emotion.EmotionWhisper, "[whispers]"},
		{emotion.EmotionReluctant, "[reluctantly]"},
		{emotion.EmotionCrying, "[crying]"},
		{emotion.EmotionVerySlow, "[very slowly]"},
		{emotion.EmotionVeryFast, "[very fast]"},
		{emotion.EmotionNeutral, ""},
	}

	for _, tt := range tests {
		if got := qwenInlineEmotionTag(tt.emotion); got != tt.want {
			t.Fatalf("qwenInlineEmotionTag(%v) = %q, want %q", tt.emotion, got, tt.want)
		}
	}
}

func TestNormalizeLeadingQwenAudioStripsWAVHeader(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	wav := makeTestWAV(payload)

	normalized, needMore, detectedWAV, err := normalizeLeadingQwenAudio(wav)
	if err != nil {
		t.Fatalf("normalizeLeadingQwenAudio error = %v", err)
	}
	if needMore {
		t.Fatal("needMore = true")
	}
	if !detectedWAV {
		t.Fatal("detectedWAV = false")
	}
	if !bytes.Equal(normalized, payload) {
		t.Fatalf("normalized = %v", normalized)
	}
}

func TestNormalizeLeadingQwenAudioKeepsPCM(t *testing.T) {
	pcm := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	normalized, needMore, detectedWAV, err := normalizeLeadingQwenAudio(pcm)
	if err != nil {
		t.Fatalf("normalizeLeadingQwenAudio error = %v", err)
	}
	if needMore || detectedWAV {
		t.Fatalf("needMore=%v detectedWAV=%v", needMore, detectedWAV)
	}
	if !bytes.Equal(normalized, pcm) {
		t.Fatalf("normalized = %v", normalized)
	}
}

func makeTestWAV(payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(payload)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(24000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(48000))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)
	return buf.Bytes()
}
