package controllers

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"xiaozhi/manager/backend/models"
)

func TestGetDoubaoVoicesByModel(t *testing.T) {
	v2 := GetDoubaoVoicesByModel("seed-tts-2.0-standard")
	if len(v2) < 400 {
		t.Fatalf("seed-tts-2.0-standard voices = %d, want >= 400", len(v2))
	}
	assertUniqueVoices(t, "seed-tts-2.0-standard", v2)
	assertHasVoice(t, "seed-tts-2.0-standard", v2, "zh_female_vv_uranus_bigtts")
	assertHasVoice(t, "seed-tts-2.0-standard", v2, "ICL_uranus_zh_female_xingganyujie_tob")
	assertHasVoice(t, "seed-tts-2.0-standard", v2, "ja_female_bv521_uranus_bigtts")

	v1 := GetDoubaoVoicesByModel("seed-tts-1.1")
	if len(v1) < 90 {
		t.Fatalf("seed-tts-1.1 voices = %d, want >= 90", len(v1))
	}
	assertUniqueVoices(t, "seed-tts-1.1", v1)
	assertHasVoice(t, "seed-tts-1.1", v1, "zh_female_cancan_mars_bigtts")
	assertHasVoice(t, "seed-tts-1.1", v1, "zh_male_M100_conversation_wvae_bigtts")

	if icl := GetDoubaoVoicesByModel("seed-icl-2.0-standard"); len(icl) != 0 {
		t.Fatalf("seed-icl-2.0-standard returned %d system voices, want 0", len(icl))
	}
}

func TestDoubaoVoiceOptionsModelRoutingAndLegacyVoiceRetention(t *testing.T) {
	db := previewTestDB(t)
	cfg := models.Config{
		Type:     "tts",
		ConfigID: "doubao_legacy_cfg",
		Provider: "doubao_ws",
		Enabled:  true,
		JsonData: `{"model":"seed-tts-2.0-standard","voice":"BV001_streaming"}`,
	}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}

	// Saved config uses 2.0 model, should return 2.0 voices AND keep legacy BV001_streaming at front.
	v2, err := getVoiceOptionsForUser(db, nil, 0, "doubao_ws", cfg.ConfigID, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(v2) == 0 || v2[0].Value != "BV001_streaming" {
		t.Fatalf("expected legacy configured voice BV001_streaming to be retained first, got %#v", v2[:minInt(3, len(v2))])
	}
	assertHasVoice(t, "doubao_ws 2.0", v2, "zh_female_vv_uranus_bigtts")
	assertHasVoice(t, "doubao_ws 2.0", v2, "ICL_uranus_zh_female_xingganyujie_tob")

	// Unsaved model override switches to 1.1 voices.
	v1, err := getVoiceOptionsForUser(db, nil, 0, "doubao_ws", cfg.ConfigID, "", "", "seed-tts-1.1")
	if err != nil {
		t.Fatal(err)
	}
	assertHasVoice(t, "doubao_ws 1.1 override", v1, "zh_female_cancan_mars_bigtts")
}

func TestResolveDoubaoModelSelectionBackend(t *testing.T) {
	sel := resolveDoubaoModelSelection("seed-tts-2.0-standard", "ICL_uranus_zh_female_xingganyujie_tob")
	if sel.ResourceID != resourceSeedTTS20Backend || sel.RequestModel != "" {
		t.Fatalf("ICL_uranus selection = %#v, want resource=%q requestModel=''", sel, resourceSeedTTS20Backend)
	}

	v1Sel := resolveDoubaoModelSelection("seed-tts-1.1", "zh_female_cancan_mars_bigtts")
	if v1Sel.ResourceID != resourceSeedTTS10Backend || v1Sel.ConfigModel != modelSeedTTS11Backend {
		t.Fatalf("1.0 bigtts selection = %#v, want resource=%q configModel=%q", v1Sel, resourceSeedTTS10Backend, modelSeedTTS11Backend)
	}
}

func TestPreviewDoubaoClonedVoiceWithAPIKeyAndDoneCode(t *testing.T) {
	var gotAPIKey, gotAppID, gotAccessKey, gotResourceID string
	chunkB64 := base64.StdEncoding.EncodeToString([]byte("fake-mp3-bytes"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotAppID = r.Header.Get("X-Api-App-Id")
		gotAccessKey = r.Header.Get("X-Api-Access-Key")
		gotResourceID = r.Header.Get("X-Api-Resource-Id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"message":"","data":"` + chunkB64 + `"}` + "\n"))
		_, _ = w.Write([]byte(`{"code":20000000,"message":"OK","data":null}` + "\n"))
	}))
	defer srv.Close()

	vcc := &VoiceCloneController{HTTPClient: srv.Client()}
	audio, contentType, err := vcc.previewDoubaoClonedVoice(context.Background(), map[string]any{
		"api_key": "new-console-key",
		"model":   "seed-tts-2.0-standard",
		"api_url": srv.URL,
	}, "ICL_uranus_zh_female_xingganyujie_tob", "你好")
	if err != nil {
		t.Fatalf("previewDoubaoClonedVoice error = %v", err)
	}
	if string(audio) != "fake-mp3-bytes" || contentType != "audio/mpeg" {
		t.Fatalf("audio=%q contentType=%q", string(audio), contentType)
	}
	if gotAPIKey != "new-console-key" || gotAppID != "" || gotAccessKey != "" {
		t.Fatalf("headers apiKey=%q appID=%q accessKey=%q", gotAPIKey, gotAppID, gotAccessKey)
	}
	if gotResourceID != "seed-tts-2.0" {
		t.Fatalf("X-Api-Resource-Id = %q, want seed-tts-2.0", gotResourceID)
	}
}

func assertUniqueVoices(t *testing.T, label string, voices []VoiceOption) {
	t.Helper()
	seen := make(map[string]bool, len(voices))
	for _, v := range voices {
		if v.Value == "" {
			t.Fatalf("%s contains empty voice value", label)
		}
		if seen[v.Value] {
			t.Fatalf("%s contains duplicate voice value %q", label, v.Value)
		}
		seen[v.Value] = true
	}
}

func assertHasVoice(t *testing.T, label string, voices []VoiceOption, want string) {
	t.Helper()
	for _, v := range voices {
		if v.Value == want {
			return
		}
	}
	t.Fatalf("%s missing expected voice %q", label, want)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
