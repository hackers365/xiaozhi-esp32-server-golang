package configprovider

import (
	"strings"
)

var knownProviders = map[string]map[string]struct{}{
	"vad": {
		"ten_vad": {}, "webrtc_vad": {}, "silero_vad": {},
	},
	"asr": {
		"funasr": {}, "aliyun_funasr": {}, "doubao": {}, "aliyun_qwen3": {}, "xunfei": {}, "local_asr_server": {},
	},
	"llm": {
		"openai": {}, "ollama": {}, "azure": {}, "anthropic": {}, "zhipu": {}, "aliyun": {},
		"doubao": {}, "siliconflow": {}, "deepseek": {}, "dify": {}, "coze": {},
	},
	"tts": {
		"doubao": {}, "doubao_ws": {}, "cosyvoice": {}, "edge": {}, "edge_offline": {},
		"xiaozhi": {}, "xunfei": {}, "xunfei_super_tts": {}, "openai": {}, "zhipu": {},
		"minimax": {}, "aliyun_qwen": {}, "indextts_vllm": {},
	},
	"memory": {
		"nomemo": {}, "memobase": {}, "mem0": {}, "memos": {},
	},
	"vision": {
		"aliyun_vision": {}, "doubao_vision": {},
	},
}

func NormalizeProvider(configType, configID string, data map[string]interface{}) string {
	configType = normalizeToken(configType)
	configID = normalizeToken(configID)

	if provider := knownProvider(configType, stringFromMap(data, "provider")); provider != "" {
		return provider
	}
	if provider := knownProvider(configType, configID); provider != "" {
		return provider
	}

	switch configType {
	case "vad":
		if provider := inferVADProvider(data); provider != "" {
			return provider
		}
	case "asr":
		if provider := inferASRProvider(data); provider != "" {
			return provider
		}
	case "llm":
		if provider := inferLLMProvider(data); provider != "" {
			return provider
		}
		return "openai"
	case "tts":
		if provider := inferTTSProvider(data); provider != "" {
			return provider
		}
	case "memory":
		if provider := inferMemoryProvider(data); provider != "" {
			return provider
		}
	case "vision":
		if provider := inferVisionProvider(data); provider != "" {
			return provider
		}
	}

	if fallback := defaultProvider(configType); fallback != "" {
		return fallback
	}

	if configID != "" {
		return configID
	}
	return normalizeToken(stringFromMap(data, "provider"))
}

func NormalizeExistingProvider(configType, provider, configID string, data map[string]interface{}) string {
	configType = normalizeToken(configType)
	if known := knownProvider(configType, provider); known != "" {
		return known
	}
	return NormalizeProvider(configType, configID, data)
}

func ExportData(configType, configID, provider string, data map[string]interface{}) map[string]interface{} {
	out := cloneMap(data)
	configType = normalizeToken(configType)
	if !isManagedProviderType(configType) {
		return out
	}

	normalized := NormalizeExistingProvider(configType, provider, configID, out)
	if normalized != "" {
		out["provider"] = normalized
	}
	if configType == "llm" {
		if _, exists := out["type"]; !exists {
			out["type"] = llmTypeForProvider(normalized)
		}
	}
	return out
}

func isManagedProviderType(configType string) bool {
	switch normalizeToken(configType) {
	case "vad", "asr", "llm", "tts", "memory", "vision":
		return true
	default:
		return false
	}
}

func defaultProvider(configType string) string {
	switch normalizeToken(configType) {
	case "vad":
		return "ten_vad"
	case "asr":
		return "funasr"
	case "llm":
		return "openai"
	case "tts":
		return "doubao_ws"
	case "memory":
		return "memobase"
	case "vision":
		return "aliyun_vision"
	default:
		return ""
	}
}

func knownProvider(configType, provider string) string {
	provider = normalizeToken(provider)
	if provider == "" {
		return ""
	}
	if providers, ok := knownProviders[normalizeToken(configType)]; ok {
		if _, exists := providers[provider]; exists {
			return provider
		}
	}
	return ""
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func stringFromMap(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if data == nil {
			return ""
		}
		if raw, ok := data[key]; ok {
			if value, ok := raw.(string); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func hasAny(data map[string]interface{}, keys ...string) bool {
	if data == nil {
		return false
	}
	for _, key := range keys {
		if _, ok := data[key]; ok {
			return true
		}
	}
	return false
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src)+2)
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func containsAny(value string, needles ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func inferVADProvider(data map[string]interface{}) string {
	if hasAny(data, "hop_size") {
		return "ten_vad"
	}
	if hasAny(data, "model_path", "min_silence_duration_ms") {
		return "silero_vad"
	}
	if hasAny(data, "vad_mode", "vad_sample_rate", "pool_min_size", "pool_max_idle") {
		return "webrtc_vad"
	}
	return ""
}

func inferASRProvider(data map[string]interface{}) string {
	if hasAny(data, "local_asr_server") {
		return "local_asr_server"
	}
	if hasAny(data, "appid", "api_secret") {
		return "xunfei"
	}
	model := stringFromMap(data, "model")
	wsURL := stringFromMap(data, "ws_url")
	if containsAny(model, "qwen3-asr") || containsAny(wsURL, "/realtime") {
		return "aliyun_qwen3"
	}
	if containsAny(model, "fun-asr") || containsAny(wsURL, "/inference") {
		return "aliyun_funasr"
	}
	if hasAny(data, "access_token", "resource_id", "end_window_size", "chunk_duration") {
		return "doubao"
	}
	if hasAny(data, "host", "port", "chunk_size", "chunk_interval", "max_connections") {
		return "funasr"
	}
	return ""
}

func inferLLMProvider(data map[string]interface{}) string {
	llmType := normalizeToken(stringFromMap(data, "type"))
	if provider := knownProvider("llm", llmType); provider != "" && provider != "openai" {
		return provider
	}

	baseURL := stringFromMap(data, "base_url", "api_url")
	switch {
	case containsAny(baseURL, "openai.azure.com"):
		return "azure"
	case containsAny(baseURL, "anthropic.com"):
		return "anthropic"
	case containsAny(baseURL, "bigmodel.cn"):
		return "zhipu"
	case containsAny(baseURL, "dashscope.aliyuncs.com"):
		return "aliyun"
	case containsAny(baseURL, "volces.com", "volcengineapi.com", "ark.cn-"):
		return "doubao"
	case containsAny(baseURL, "siliconflow.cn"):
		return "siliconflow"
	case containsAny(baseURL, "deepseek.com"):
		return "deepseek"
	case containsAny(baseURL, "localhost:11434", "127.0.0.1:11434"):
		return "ollama"
	}

	if llmType == "openai" || llmType == "" {
		return "openai"
	}
	return ""
}

func inferTTSProvider(data map[string]interface{}) string {
	provider := knownProvider("tts", stringFromMap(data, "provider"))
	if provider != "" {
		return provider
	}

	apiURL := stringFromMap(data, "api_url", "server_url", "ws_url")
	model := normalizeToken(stringFromMap(data, "model"))
	switch {
	case hasAny(data, "spk_id", "instruct_text"):
		return "cosyvoice"
	case hasAny(data, "server_url"):
		return "edge_offline"
	case hasAny(data, "rate", "pitch") && hasAny(data, "voice"):
		return "edge"
	case containsAny(apiURL, "xfyun.cn"):
		if hasAny(data, "double_stream", "bgs", "oral_level", "spark_assist", "stop_split", "remain") {
			return "xunfei_super_tts"
		}
		return "xunfei"
	case containsAny(apiURL, "dashscope.aliyuncs.com") || containsAny(model, "qwen") || hasAny(data, "language_type", "region"):
		return "aliyun_qwen"
	case containsAny(apiURL, "bigmodel.cn") || containsAny(model, "glm-tts"):
		return "zhipu"
	case containsAny(apiURL, "minimax"):
		return "minimax"
	case containsAny(model, "indextts"):
		return "indextts_vllm"
	case containsAny(apiURL, "openspeech", "volces.com", "volcengine"):
		if hasAny(data, "ws_url", "ws_host", "use_stream", "resource_id") {
			return "doubao_ws"
		}
		return "doubao"
	case hasAny(data, "api_key", "model", "voice", "response_format"):
		return "openai"
	}
	return ""
}

func inferMemoryProvider(data map[string]interface{}) string {
	baseURL := stringFromMap(data, "base_url")
	switch {
	case containsAny(baseURL, "memobase"):
		return "memobase"
	case containsAny(baseURL, "mem0.ai"):
		return "mem0"
	case containsAny(baseURL, "memos", "memtensor"):
		return "memos"
	}
	return ""
}

func inferVisionProvider(data map[string]interface{}) string {
	baseURL := stringFromMap(data, "base_url", "api_url")
	model := stringFromMap(data, "model_name", "model")
	switch {
	case containsAny(baseURL, "dashscope.aliyuncs.com") || containsAny(model, "qwen-vl"):
		return "aliyun_vision"
	case containsAny(baseURL, "volces.com", "volcengine", "ark.cn-") || containsAny(model, "doubao"):
		return "doubao_vision"
	}
	return ""
}

func llmTypeForProvider(provider string) string {
	switch normalizeToken(provider) {
	case "ollama":
		return "ollama"
	case "dify":
		return "dify"
	case "coze":
		return "coze"
	default:
		return "openai"
	}
}
