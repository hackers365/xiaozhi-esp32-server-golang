package controllers

import "strings"

const (
	defaultDoubaoTTSModelBackend  = "seed-tts-1.1"
	resourceSeedTTS10Backend      = "seed-tts-1.0"
	resourceSeedTTS20Backend      = "seed-tts-2.0"
	resourceSeedICL10Backend      = "seed-icl-1.0"
	resourceSeedICL20Backend      = "seed-icl-2.0"
	modelSeedTTS11Backend         = "seed-tts-1.1"
	modelSeedTTS20StandardBackend = "seed-tts-2.0-standard"
	modelSeedTTS20ExprBackend     = "seed-tts-2.0-expressive"
	modelSeedICL10Backend         = "seed-icl-1.0"
	modelSeedICL20StandardBackend = "seed-icl-2.0-standard"
	modelSeedICL20ExprBackend     = "seed-icl-2.0-expressive"
)

type doubaoModelSelection struct {
	ConfigModel  string
	RequestModel string
	ResourceID   string
}

func normalizeDoubaoModelBackend(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	switch model {
	case "", "default":
		return ""
	case strings.ToLower(modelSeedTTS11Backend):
		return modelSeedTTS11Backend
	case strings.ToLower(modelSeedTTS20StandardBackend), "seed-tts-2.0":
		return modelSeedTTS20StandardBackend
	case strings.ToLower(modelSeedTTS20ExprBackend):
		return modelSeedTTS20ExprBackend
	case strings.ToLower(modelSeedICL10Backend):
		return modelSeedICL10Backend
	case strings.ToLower(modelSeedICL20StandardBackend):
		return modelSeedICL20StandardBackend
	case strings.ToLower(modelSeedICL20ExprBackend):
		return modelSeedICL20ExprBackend
	default:
		return strings.TrimSpace(model)
	}
}

func inferDoubaoVoiceFamilyBackend(voice string) string {
	voice = strings.TrimSpace(strings.ToLower(voice))
	switch {
	case voice == "":
		return "unknown"
	case strings.HasPrefix(voice, "icl_uranus_"):
		return "tts2"
	case strings.HasPrefix(voice, "saturn_"):
		return "tts2"
	case strings.HasPrefix(voice, "s_"):
		return "icl1"
	case strings.HasPrefix(voice, "icl_"):
		return "icl1"
	case strings.Contains(voice, "_uranus_bigtts"):
		return "tts2"
	case strings.Contains(voice, "_bigtts"):
		return "tts1_bigtts"
	default:
		return "tts1"
	}
}

func resolveDoubaoModelSelection(model, voice string) doubaoModelSelection {
	voiceFamily := inferDoubaoVoiceFamilyBackend(voice)
	normalized := normalizeDoubaoModelBackend(model)
	if voiceFamily == "tts2" && normalized == modelSeedTTS11Backend {
		normalized = modelSeedTTS20StandardBackend
	}
	if voiceFamily == "tts1_bigtts" && (normalized == modelSeedTTS20StandardBackend || normalized == modelSeedTTS20ExprBackend) {
		normalized = modelSeedTTS11Backend
	}
	if normalized == "" {
		switch voiceFamily {
		case "icl2":
			return doubaoModelSelection{
				ConfigModel:  modelSeedICL20ExprBackend,
				RequestModel: modelSeedTTS20ExprBackend,
				ResourceID:   resourceSeedICL20Backend,
			}
		case "icl1":
			return doubaoModelSelection{
				ConfigModel:  modelSeedICL10Backend,
				RequestModel: "",
				ResourceID:   resourceSeedICL10Backend,
			}
		case "tts2":
			return doubaoModelSelection{
				ConfigModel:  modelSeedTTS20StandardBackend,
				RequestModel: "",
				ResourceID:   resourceSeedTTS20Backend,
			}
		default:
			return doubaoModelSelection{
				ConfigModel:  defaultDoubaoTTSModelBackend,
				RequestModel: modelSeedTTS11Backend,
				ResourceID:   resourceSeedTTS10Backend,
			}
		}
	}

	switch normalized {
	case modelSeedTTS11Backend:
		if inferDoubaoVoiceFamilyBackend(voice) == "tts2" {
			return doubaoModelSelection{ConfigModel: modelSeedTTS20StandardBackend, RequestModel: "", ResourceID: resourceSeedTTS20Backend}
		}
		return doubaoModelSelection{ConfigModel: normalized, RequestModel: modelSeedTTS11Backend, ResourceID: resourceSeedTTS10Backend}
	case modelSeedTTS20StandardBackend:
		if inferDoubaoVoiceFamilyBackend(voice) == "tts2" {
			return doubaoModelSelection{ConfigModel: normalized, RequestModel: "", ResourceID: resourceSeedTTS20Backend}
		}
		return doubaoModelSelection{ConfigModel: normalized, RequestModel: modelSeedTTS20StandardBackend, ResourceID: resourceSeedTTS20Backend}
	case modelSeedTTS20ExprBackend:
		if inferDoubaoVoiceFamilyBackend(voice) == "tts2" {
			return doubaoModelSelection{ConfigModel: normalized, RequestModel: "", ResourceID: resourceSeedTTS20Backend}
		}
		return doubaoModelSelection{ConfigModel: normalized, RequestModel: modelSeedTTS20ExprBackend, ResourceID: resourceSeedTTS20Backend}
	case modelSeedICL10Backend:
		return doubaoModelSelection{ConfigModel: normalized, RequestModel: "", ResourceID: resourceSeedICL10Backend}
	case modelSeedICL20StandardBackend:
		return doubaoModelSelection{ConfigModel: normalized, RequestModel: modelSeedTTS20StandardBackend, ResourceID: resourceSeedICL20Backend}
	case modelSeedICL20ExprBackend:
		return doubaoModelSelection{ConfigModel: normalized, RequestModel: modelSeedTTS20ExprBackend, ResourceID: resourceSeedICL20Backend}
	default:
		return doubaoModelSelection{ConfigModel: normalized, RequestModel: normalized, ResourceID: resourceSeedTTS10Backend}
	}
}

func resolveDoubaoCloneTargetModel(model string) (modelType int, targetModel string) {
	switch normalizeDoubaoModelBackend(model) {
	case modelSeedTTS20StandardBackend, modelSeedICL20StandardBackend:
		return 4, modelSeedICL20StandardBackend
	case modelSeedTTS20ExprBackend, modelSeedICL20ExprBackend:
		return 4, modelSeedICL20ExprBackend
	default:
		return 1, modelSeedICL10Backend
	}
}
