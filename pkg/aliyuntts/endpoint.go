package aliyuntts

import (
	"errors"
	"net/url"
	"strings"
)

const (
	MultimodalGenerationPath = "/api/v1/services/aigc/multimodal-generation/generation"
	SpeechSynthesizerPath    = "/api/v1/services/audio/tts/SpeechSynthesizer"
)

// ResolveHTTPAPIURL selects the HTTP protocol used by each Aliyun TTS model.
// It only rewrites official DashScope endpoints; custom compatible endpoints
// are preserved as configured.
func ResolveHTTPAPIURL(rawURL, model string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", errors.New("invalid Aliyun TTS API URL")
	}

	host := strings.ToLower(parsed.Hostname())
	if !isOfficialDashScopeHost(host) {
		return parsed.String(), nil
	}

	capability := GetAliyunModelCapability(model)
	switch capability.Category {
	case CategoryQwenAudio, CategoryCosyVoice:
		if host == "dashscope-intl.aliyuncs.com" || strings.Contains(host, ".ap-southeast-1.") {
			return "", errors.New("Qwen-Audio-TTS and CosyVoice non-realtime HTTP synthesis are only available in the Beijing region")
		}
		parsed.Path = SpeechSynthesizerPath
	case CategoryQwenTTS:
		parsed.Path = MultimodalGenerationPath
	default:
		return parsed.String(), nil
	}
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func isOfficialDashScopeHost(host string) bool {
	return host == "dashscope.aliyuncs.com" ||
		host == "dashscope-intl.aliyuncs.com" ||
		strings.HasSuffix(host, ".maas.aliyuncs.com")
}
