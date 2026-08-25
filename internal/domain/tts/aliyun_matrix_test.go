package tts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAliyunCapabilityWrapperPreservesJSONAndStringCompatibility(t *testing.T) {
	capability := GetAliyunModelCapability("qwen3-tts-flash")
	var emotionMode string = capability.EmotionMode
	if emotionMode != "instruction" {
		t.Fatalf("EmotionMode = %q", emotionMode)
	}
	data, err := json.Marshal(capability)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	for _, key := range []string{`"model_id"`, `"supports_voice_clone"`, `"supports_instruction"`, `"emotion_mode"`, `"default_sample_rate"`} {
		if !strings.Contains(encoded, key) {
			t.Fatalf("JSON %s missing %s", encoded, key)
		}
	}
}
