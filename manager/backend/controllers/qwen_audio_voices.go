package controllers

import (
	_ "embed"
	"encoding/json"
)

const (
	qwenAudioFlashModel = "qwen-audio-3.0-tts-flash"
	qwenAudioPlusModel  = "qwen-audio-3.0-tts-plus"
)

type qwenAudioVoiceSpec struct {
	Suffix      string   `json:"suffix"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Languages   []string `json:"languages"`
}

// Generated from the two Qwen Audio system-voice spreadsheets supplied with
// the project update. Both models share metadata and differ only in the voice
// parameter's model prefix.
//
//go:embed qwen_audio_voices.json
var qwenAudioVoiceData []byte

func init() {
	var specs []qwenAudioVoiceSpec
	if err := json.Unmarshal(qwenAudioVoiceData, &specs); err != nil {
		panic("parse embedded Qwen Audio voices: " + err.Error())
	}
	ModelVoiceMap[qwenAudioFlashModel] = buildQwenAudioVoices(qwenAudioFlashModel, specs)
	ModelVoiceMap[qwenAudioPlusModel] = buildQwenAudioVoices(qwenAudioPlusModel, specs)
}

func buildQwenAudioVoices(model string, specs []qwenAudioVoiceSpec) []VoiceInfo {
	voices := make([]VoiceInfo, 0, len(specs))
	for _, spec := range specs {
		voices = append(voices, VoiceInfo{
			Value:       model + "-" + spec.Suffix,
			Label:       spec.Label,
			Description: spec.Description,
			Languages:   spec.Languages,
		})
	}
	return voices
}
