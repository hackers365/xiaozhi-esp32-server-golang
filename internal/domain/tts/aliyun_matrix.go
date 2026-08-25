package tts

import "xiaozhi-esp32-server-golang/pkg/aliyuntts"

// AliyunTTSModelCapability preserves the original domain API while the shared
// classifier lives in a package that the Manager module can also import.
type AliyunTTSModelCapability = aliyuntts.ModelCapability

func GetAliyunModelCapability(model string) AliyunTTSModelCapability {
	return aliyuntts.GetAliyunModelCapability(model)
}
