package controllers

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"xiaozhi/manager/backend/models"
)

type previewRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn previewRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func previewTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Config{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestResolveUserPreviewUsesOnlyEnabledAliyunConfig(t *testing.T) {
	db := previewTestDB(t)
	config := models.Config{
		Type: "tts", ConfigID: "aliyun_enabled", Provider: "aliyun_qwen", Enabled: true,
		JsonData: `{"api_key":"stored-key","api_url":"https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation","model":"qwen3-tts-flash","voice":"Ethan"}`,
	}
	if err := db.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	controller := NewTTSPreviewController(db)

	resolved, err := controller.resolvePreviewRequest(TTSPreviewRequest{
		Provider: "aliyun_qwen", ConfigID: config.ConfigID, APIKey: "attacker-key", APIURL: "https://example.com/private", Model: "attacker-model", Voice: "Serena",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.APIKey != "stored-key" || resolved.APIURL != "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation" || resolved.Model != "qwen3-tts-flash" {
		t.Fatalf("user overrides leaked into resolved config: %#v", resolved)
	}
	if resolved.Voice != "Serena" {
		t.Fatalf("role voice override = %q", resolved.Voice)
	}
	storedVoice, err := controller.resolvePreviewRequest(TTSPreviewRequest{Provider: "aliyun_qwen", ConfigID: config.ConfigID}, true)
	if err != nil {
		t.Fatal(err)
	}
	if storedVoice.Voice != "Ethan" {
		t.Fatalf("stored voice = %q", storedVoice.Voice)
	}
}

func TestResolveUserPreviewRejectsDisabledAndNonAliyunConfigs(t *testing.T) {
	db := previewTestDB(t)
	configs := []models.Config{
		{Type: "tts", ConfigID: "disabled", Provider: "aliyun_qwen", Enabled: false, JsonData: `{"api_key":"key"}`},
		{Type: "tts", ConfigID: "minimax", Provider: "minimax", Enabled: true, JsonData: `{"api_key":"key"}`},
	}
	if err := db.Create(&configs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Config{}).Where("config_id = ?", "disabled").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	controller := NewTTSPreviewController(db)
	for _, configID := range []string{"disabled", "minimax"} {
		if _, err := controller.resolvePreviewRequest(TTSPreviewRequest{Provider: "aliyun_qwen", ConfigID: configID}, true); err == nil {
			t.Fatalf("config %q unexpectedly allowed", configID)
		}
	}
}

func TestResolveAdminPreviewUsesRegionEndpointAndRejectsOtherProviders(t *testing.T) {
	controller := NewTTSPreviewController(nil)
	resolved, err := controller.resolvePreviewRequest(TTSPreviewRequest{Provider: "aliyun_qwen", Region: "singapore", APIKey: "key", Model: "qwen3-tts-flash"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.APIURL != defaultAliyunPreviewAPIURLSingapore {
		t.Fatalf("APIURL = %q", resolved.APIURL)
	}
	if _, err := controller.resolvePreviewRequest(TTSPreviewRequest{Provider: "cosyvoice", APIKey: "key"}, false); err == nil {
		t.Fatal("local cosyvoice preview unexpectedly allowed")
	}
}

func TestAliyunPreviewURLValidation(t *testing.T) {
	for _, raw := range []string{
		"http://dashscope.aliyuncs.com/api",
		"https://127.0.0.1/api",
		"https://localhost/api",
		"https://example.com/api",
	} {
		if err := validateAliyunPreviewURL(raw, false); err == nil {
			t.Fatalf("API URL %q unexpectedly allowed", raw)
		}
	}
	if err := validateAliyunPreviewURL("https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation", false); err != nil {
		t.Fatal(err)
	}
	if err := validateAliyunPreviewURL("https://dashscope-result.oss-cn-shanghai.aliyuncs.com/audio.wav", true); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAliyunPreviewRequestBodyUsesModelCapabilities(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		instruction  string
		wantModel    string
		wantPlural   bool
		wantSingular bool
	}{
		{name: "qwen flash upgrades", model: "qwen3-tts-flash", instruction: "warm voice", wantModel: "qwen3-tts-instruct-flash", wantPlural: true},
		{name: "qwen audio accepts instruction", model: "qwen-audio-3.0-tts-flash", instruction: "warm voice", wantModel: "qwen-audio-3.0-tts-flash", wantSingular: true},
		{name: "legacy qwen instruct accepts instruction", model: "qwen-tts-instruct", instruction: "warm voice", wantModel: "qwen-tts-instruct", wantPlural: true},
		{name: "versioned qwen flash is not silently changed", model: "qwen3-tts-flash-2025-11-27", instruction: "warm voice", wantModel: "qwen3-tts-flash-2025-11-27"},
		{name: "aliyun cosyvoice rejects qwen instruction", model: "cosyvoice-v3-flash", instruction: "warm voice", wantModel: "cosyvoice-v3-flash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := buildAliyunPreviewRequestBody(TTSPreviewRequest{Model: tt.model, Voice: "Cherry", Text: "hello", Instruction: tt.instruction, Format: "ogg_opus"})
			if body["model"] != tt.wantModel {
				t.Fatalf("model = %v", body["model"])
			}
			input := body["input"].(map[string]any)
			_, hasPlural := input["instructions"]
			if hasPlural != tt.wantPlural {
				t.Fatalf("input = %#v", input)
			}
			_, hasSingular := input["instruction"]
			if hasSingular != tt.wantSingular {
				t.Fatalf("input = %#v", input)
			}
			if strings.HasPrefix(tt.model, "qwen-audio-") {
				if input["format"] != "opus" || input["sample_rate"] != 24000 {
					t.Fatalf("SpeechSynthesizer fields = %#v", input)
				}
			}
		})
	}
}

func TestQwenTTSOfficialPreviewNeverAppliesToQwenAudio(t *testing.T) {
	if got := qwenTTSOfficialPreviewURL("qwen3-tts-flash", "Cherry"); got == "" {
		t.Fatal("Qwen-TTS Cherry official preview URL is missing")
	}
	if got := qwenTTSOfficialPreviewURL("qwen-audio-3.0-tts-flash", "Cherry"); got != "" {
		t.Fatalf("Qwen-Audio unexpectedly reused Qwen-TTS preview: %q", got)
	}
}

func TestPreviewAliyunTTSUsesOfficialQwenTTSWaveWithoutSynthesis(t *testing.T) {
	controller := NewTTSPreviewController(nil)
	controller.HTTPClient = &http.Client{Transport: previewRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Hostname() != "help-static-aliyun-doc.aliyuncs.com" {
			t.Fatalf("unexpected official preview request: %s %s", req.Method, req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"audio/wav"}},
			Body:       io.NopCloser(strings.NewReader("RIFF-test")),
			Request:    req,
		}, nil
	})}

	audio, contentType, err := controller.previewAliyunTTS(context.Background(), "", defaultAliyunPreviewAPIURLBeijing, TTSPreviewRequest{
		Model: "qwen3-tts-flash", Voice: "Cherry", Text: "ignored for official system preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "RIFF-test" || contentType != "audio/wav" {
		t.Fatalf("audio=%q contentType=%q", audio, contentType)
	}
}

func TestLoadQwenAudioOfficialPreviewUsesOnlyItsOwnVoicePack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XIAOZHI_QWEN_AUDIO_PREVIEW_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "longcanzhuyue.wav"), []byte("RIFF-qwen-audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	audio, found, err := loadQwenAudioOfficialPreview(
		"qwen-audio-3.0-tts-flash",
		"qwen-audio-3.0-tts-flash-longcanzhuyue",
	)
	if err != nil || !found || string(audio) != "RIFF-qwen-audio" {
		t.Fatalf("audio=%q found=%v err=%v", audio, found, err)
	}
	if _, found, err := loadQwenAudioOfficialPreview("qwen3-tts-flash", "Cherry"); err != nil || found {
		t.Fatalf("Qwen-TTS unexpectedly used Qwen-Audio pack: found=%v err=%v", found, err)
	}
	if _, found, err := loadQwenAudioOfficialPreview("qwen-audio-3.0-tts-flash", "../longcanzhuyue"); err != nil || found {
		t.Fatalf("unsafe voice path result: found=%v err=%v", found, err)
	}
}
