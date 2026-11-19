package controllers

import (
	_ "embed"
	"encoding/json"
	"strings"
)

type doubaoVoiceCatalog struct {
	TTS20 []VoiceOption `json:"tts_2_0"`
	TTS10 []VoiceOption `json:"tts_1_0"`
}

// 官方豆包语音在线音色列表（来源：https://docs.volcengine.com/docs/doubao-voice/tone-list-3）
//
//go:embed doubao_voices.json
var doubaoVoiceCatalogData []byte

var parsedDoubaoVoices doubaoVoiceCatalog

func init() {
	if err := json.Unmarshal(doubaoVoiceCatalogData, &parsedDoubaoVoices); err != nil {
		panic("parse embedded Doubao voices: " + err.Error())
	}
	if len(parsedDoubaoVoices.TTS20) > 0 {
		VoiceOptions["doubao_ws"] = append([]VoiceOption(nil), parsedDoubaoVoices.TTS20...)
		VoiceOptions["doubao"] = append([]VoiceOption(nil), parsedDoubaoVoices.TTS20...)
	}
}

// GetDoubaoVoicesByModel 根据豆包 TTS 模型返回对应的官方在线音色列表。
// - seed-tts-2.0*（及未指定模型时的默认情况）返回豆包语音合成模型 2.0 音色表；
// - seed-tts-1.1 / seed-tts-1.0 返回豆包语音合成模型 1.0 音色表；
// - seed-icl-* 为声音复刻模型，不返回公版系统音色（由复刻音色与自定义输入提供）。
func GetDoubaoVoicesByModel(model string) []VoiceOption {
	normalized := normalizeDoubaoModelBackend(model)
	switch normalized {
	case "", modelSeedTTS20StandardBackend, modelSeedTTS20ExprBackend:
		return append([]VoiceOption(nil), parsedDoubaoVoices.TTS20...)
	case modelSeedTTS11Backend, resourceSeedTTS10Backend:
		return append([]VoiceOption(nil), parsedDoubaoVoices.TTS10...)
	case modelSeedICL10Backend, modelSeedICL20StandardBackend, modelSeedICL20ExprBackend, resourceSeedICL20Backend:
		return []VoiceOption{}
	default:
		if strings.HasPrefix(strings.ToLower(normalized), "seed-icl-") {
			return []VoiceOption{}
		}
		if strings.HasPrefix(strings.ToLower(normalized), "seed-tts-1.") {
			return append([]VoiceOption(nil), parsedDoubaoVoices.TTS10...)
		}
		return append([]VoiceOption(nil), parsedDoubaoVoices.TTS20...)
	}
}
