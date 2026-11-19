package doubao

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewDoubaoTTSProvider(t *testing.T) {
	config := map[string]interface{}{
		"api_key":      "test_api_key",
		"appid":        "test_app_id",
		"access_token": "test_token",
		"model":        "seed-tts-1.1",
		"voice":        "BV001_streaming",
		"api_url":      "https://api.test.com/v3/tts/unidirectional",
	}

	provider := NewDoubaoTTSProvider(config)
	if provider.APIKey != "test_api_key" {
		t.Fatalf("APIKey = %q", provider.APIKey)
	}
	if provider.AppID != "test_app_id" {
		t.Fatalf("AppID = %q", provider.AppID)
	}
	if provider.AccessToken != "test_token" {
		t.Fatalf("AccessToken = %q", provider.AccessToken)
	}
	if provider.Model != "seed-tts-1.1" {
		t.Fatalf("Model = %q", provider.Model)
	}
	if provider.Voice != "BV001_streaming" {
		t.Fatalf("Voice = %q", provider.Voice)
	}
	if provider.APIURL != "https://api.test.com/v3/tts/unidirectional" {
		t.Fatalf("APIURL = %q", provider.APIURL)
	}
}

func TestResolveDoubaoTTSModelDefaults(t *testing.T) {
	got, err := resolveDoubaoTTSModel("", "BV001_streaming")
	if err != nil {
		t.Fatalf("resolveDoubaoTTSModel default error = %v", err)
	}
	if got.ConfigModel != defaultDoubaoTTSModel {
		t.Fatalf("ConfigModel = %q", got.ConfigModel)
	}
	if got.ResourceID != resourceSeedTTS10 {
		t.Fatalf("ResourceID = %q", got.ResourceID)
	}
	if got.RequestModel != modelSeedTTS11 {
		t.Fatalf("RequestModel = %q", got.RequestModel)
	}
}

func TestResolveDoubaoTTSModelCloneVoice(t *testing.T) {
	got, err := resolveDoubaoTTSModel("", "ICL_demo_voice")
	if err != nil {
		t.Fatalf("resolveDoubaoTTSModel clone error = %v", err)
	}
	if got.ResourceID != resourceSeedICL10 {
		t.Fatalf("ResourceID = %q", got.ResourceID)
	}
	if got.ConfigModel != modelSeedICL10 {
		t.Fatalf("ConfigModel = %q", got.ConfigModel)
	}
}

func TestResolveDoubaoTTSModelSaturnVoiceDefaultsToTTS20(t *testing.T) {
	got, err := resolveDoubaoTTSModel("", "saturn_zh_female_cancan_tob")
	if err != nil {
		t.Fatalf("resolveDoubaoTTSModel saturn error = %v", err)
	}
	if got.ResourceID != resourceSeedTTS20 {
		t.Fatalf("ResourceID = %q", got.ResourceID)
	}
	if got.ConfigModel != modelSeedTTS20Standard {
		t.Fatalf("ConfigModel = %q", got.ConfigModel)
	}
}

func TestResolveDoubaoTTSModelUranusAndICLUranusDefaultToTTS20(t *testing.T) {
	cases := []string{
		"zh_female_vv_uranus_bigtts",
		"ICL_uranus_zh_female_xingganyujie_tob",
		"ja_female_bv521_uranus_bigtts",
	}
	for _, voice := range cases {
		got, err := resolveDoubaoTTSModel(modelSeedTTS20Standard, voice)
		if err != nil {
			t.Fatalf("resolveDoubaoTTSModel(%q) error = %v", voice, err)
		}
		if got.ResourceID != resourceSeedTTS20 {
			t.Fatalf("voice %q ResourceID = %q, want %q", voice, got.ResourceID, resourceSeedTTS20)
		}
		if got.RequestModel != "" {
			t.Fatalf("voice %q RequestModel = %q, want empty", voice, got.RequestModel)
		}
		if got.IsCloneVoice {
			t.Fatalf("voice %q unexpectedly marked as clone voice", voice)
		}
	}
}

func TestResolveDoubaoTTSModelUpgradesLegacyTTS11ForUranusVoice(t *testing.T) {
	got, err := resolveDoubaoTTSModel(modelSeedTTS11, "zh_female_vv_uranus_bigtts")
	if err != nil {
		t.Fatalf("resolveDoubaoTTSModel legacy tts1.1 error = %v", err)
	}
	if got.ResourceID != resourceSeedTTS20 {
		t.Fatalf("ResourceID = %q", got.ResourceID)
	}
	if got.ConfigModel != modelSeedTTS20Standard {
		t.Fatalf("ConfigModel = %q", got.ConfigModel)
	}
}

func TestResolveDoubaoTTSModelV1BigTTSVoiceUsesTTS10(t *testing.T) {
	for _, model := range []string{"", modelSeedTTS11, modelSeedTTS20Standard} {
		got, err := resolveDoubaoTTSModel(model, "zh_female_qinqienvsheng_moon_bigtts")
		if err != nil {
			t.Fatalf("resolveDoubaoTTSModel(%q, moon_bigtts) error = %v", model, err)
		}
		if got.ResourceID != resourceSeedTTS10 {
			t.Fatalf("model=%q ResourceID = %q, want %q", model, got.ResourceID, resourceSeedTTS10)
		}
		if got.ConfigModel != modelSeedTTS11 {
			t.Fatalf("model=%q ConfigModel = %q, want %q", model, got.ConfigModel, modelSeedTTS11)
		}
	}
}

func TestParseDoubaoTTSStreamEventDoneCode(t *testing.T) {
	event, err := parseDoubaoTTSStreamEvent([]byte(`{"code":20000000,"message":"OK","data":null}`))
	if err != nil {
		t.Fatalf("parseDoubaoTTSStreamEvent done code error = %v", err)
	}
	if event == nil || event.Code != doubaoTTSStreamDoneCode {
		t.Fatalf("event = %#v, want code %d", event, doubaoTTSStreamDoneCode)
	}
}

func TestResolveDoubaoTTSModelRejectsPublicVoiceWithICL(t *testing.T) {
	if _, err := resolveDoubaoTTSModel(modelSeedICL10, "BV001_streaming"); err == nil {
		t.Fatal("expected public voice with ICL model to fail")
	}
}

func TestBuildDoubaoWSAttemptModelsWithExplicitResourceAddsFallback(t *testing.T) {
	derived, err := resolveDoubaoTTSModel(modelSeedTTS20Standard, "zh_female_vv_uranus_bigtts")
	if err != nil {
		t.Fatalf("resolveDoubaoTTSModel error = %v", err)
	}

	got := buildDoubaoWSAttemptModels(derived, "TTS-SeedTTS2.02000000628041826146", "zh_female_vv_uranus_bigtts")
	if len(got) != 3 {
		t.Fatalf("attempt model len = %d", len(got))
	}
	if got[0].ResourceID != "TTS-SeedTTS2.02000000628041826146" {
		t.Fatalf("first resource = %q", got[0].ResourceID)
	}
	if got[1].ResourceID != resourceSeedTTS20 {
		t.Fatalf("second resource = %q", got[1].ResourceID)
	}
	if got[2].ResourceID != resourceSeedTTS10 {
		t.Fatalf("third resource = %q", got[2].ResourceID)
	}
}

func TestSummarizeDoubaoWSAttemptErrorResourceNotGranted(t *testing.T) {
	err := summarizeDoubaoWSAttemptError(
		"zh_female_vv_uranus_bigtts",
		resolvedTTSModel{ConfigModel: modelSeedTTS20Standard, ResourceID: "TTS-SeedTTS2.02000000628041826146"},
		[]string{"TTS-SeedTTS2.02000000628041826146"},
		[]error{errors.New(`建立豆包 WebSocket TTS 连接失败: websocket handshake status=403 body={"error":"[resource_id=TTS-SeedTTS2.02000000628041826146] requested resource not granted"}`)},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "resource_id 未授权") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestSummarizeDoubaoWSAttemptErrorExplicitUnauthorizedThenMismatch(t *testing.T) {
	err := summarizeDoubaoWSAttemptError(
		"zh_female_vv_uranus_bigtts",
		resolvedTTSModel{ConfigModel: modelSeedTTS20Standard, ResourceID: "TTS-SeedTTS2.02000000628041826146"},
		[]string{"TTS-SeedTTS2.02000000628041826146", resourceSeedTTS20},
		[]error{
			errors.New(`建立豆包 WebSocket TTS 连接失败: websocket handshake status=403 body={"error":"[resource_id=TTS-SeedTTS2.02000000628041826146] requested resource not granted"}`),
			errors.New(`resource ID is mismatched with speaker related resource`),
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "显式 resource_id 对当前凭据") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestNewDoubaoTTSV3RequestUsesNestedReqParams(t *testing.T) {
	req := newDoubaoTTSV3Request("你好", "voice-demo", 24000, modelSeedTTS11)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal request error = %v", err)
	}

	if _, exists := payload["text"]; exists {
		t.Fatal("unexpected top-level text field")
	}
	reqParams, ok := payload["req_params"].(map[string]any)
	if !ok {
		t.Fatalf("req_params missing: %#v", payload)
	}
	if reqParams["text"] != "你好" {
		t.Fatalf("req_params.text = %#v", reqParams["text"])
	}
	if reqParams["speaker"] != "voice-demo" {
		t.Fatalf("req_params.speaker = %#v", reqParams["speaker"])
	}
	audioParams, ok := reqParams["audio_params"].(map[string]any)
	if !ok {
		t.Fatalf("audio_params missing: %#v", reqParams)
	}
	if audioParams["format"] != defaultDoubaoAudioFmt {
		t.Fatalf("audio_params.format = %#v", audioParams["format"])
	}
}

func TestLiveDoubaoTTSWithAPIKey(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("DOUBAO_LIVE_API_KEY"))
	if apiKey == "" {
		t.Skip("set DOUBAO_LIVE_API_KEY to run live Doubao TTS verification")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsCases := []struct {
		name  string
		model string
		voice string
	}{
		{name: "ws_2.0_uranus", model: modelSeedTTS20Standard, voice: "zh_female_vv_uranus_bigtts"},
		{name: "ws_2.0_icl_uranus", model: modelSeedTTS20Standard, voice: "ICL_uranus_zh_female_xingganyujie_tob"},
		{name: "ws_1.1_mars", model: modelSeedTTS11, voice: "zh_female_cancan_mars_bigtts"},
	}
	for _, tc := range wsCases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewDoubaoWSProvider(map[string]interface{}{
				"api_key": apiKey,
				"model":   tc.model,
				"voice":   tc.voice,
			})
			frames, err := p.TextToSpeech(ctx, "你好，测试豆包语音合成。", 24000, 1, 60)
			if err != nil {
				t.Fatalf("WS TextToSpeech failed: %v", err)
			}
			if len(frames) == 0 {
				t.Fatal("WS TextToSpeech returned 0 opus frames")
			}
			t.Logf("%s: received %d opus frames", tc.name, len(frames))
		})
	}

	httpCases := []struct {
		name  string
		model string
		voice string
	}{
		{name: "http_2.0_uranus", model: modelSeedTTS20Standard, voice: "zh_female_vv_uranus_bigtts"},
		{name: "http_2.0_icl_uranus", model: modelSeedTTS20Standard, voice: "ICL_uranus_zh_female_xingganyujie_tob"},
		{name: "http_1.1_mars", model: modelSeedTTS11, voice: "zh_female_cancan_mars_bigtts"},
	}
	for _, tc := range httpCases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewDoubaoTTSProvider(map[string]interface{}{
				"api_key": apiKey,
				"model":   tc.model,
				"voice":   tc.voice,
			})
			frames, err := p.TextToSpeech(ctx, "你好，测试豆包语音合成。", 24000, 1, 60)
			if err != nil {
				t.Fatalf("HTTP TextToSpeech failed: %v", err)
			}
			if len(frames) == 0 {
				t.Fatal("HTTP TextToSpeech returned 0 opus frames")
			}
			t.Logf("%s: received %d opus frames", tc.name, len(frames))
		})
	}
}
