package controllers

import (
	"testing"

	"xiaozhi/manager/backend/models"
)

func TestVoiceOptionsUseUnsavedModelOverride(t *testing.T) {
	db := previewTestDB(t)
	cfg := models.Config{
		Type: "tts", ConfigID: "aliyun_saved_flash", Provider: "aliyun_qwen", Enabled: true,
		JsonData: `{"model":"qwen3-tts-flash"}`,
	}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}

	saved, err := getVoiceOptionsForUser(db, nil, 0, "aliyun_qwen", cfg.ConfigID, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	overridden, err := getVoiceOptionsForUser(db, nil, 0, "aliyun_qwen", cfg.ConfigID, "", "", "qwen-audio-3.0-tts-flash")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 || len(overridden) == 0 || saved[0].Value == overridden[0].Value {
		t.Fatalf("saved voices = %#v, override voices = %#v", saved, overridden)
	}
}

func TestOpenAIVoiceOptionsIncludeConfiguredCustomVoice(t *testing.T) {
	db := previewTestDB(t)
	cfg := models.Config{
		Type: "tts", ConfigID: "openai_custom_voice", Provider: "openai", Enabled: true,
		JsonData: `{"model":"gpt-4o-mini-tts","voice":"custom_voice_id"}`,
	}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}

	voices, err := getVoiceOptionsForUser(db, nil, 0, "openai", cfg.ConfigID, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	foundCustom := false
	foundAlloy := false
	for _, v := range voices {
		if v.Value == "custom_voice_id" {
			foundCustom = true
		}
		if v.Value == "alloy" {
			foundAlloy = true
			if v.Label == "alloy" {
				t.Fatalf("expected descriptive label for built-in voice alloy, got %q", v.Label)
			}
		}
	}
	if !foundCustom || !foundAlloy {
		t.Fatalf("voices = %#v, want both custom_voice_id and alloy", voices)
	}
}

