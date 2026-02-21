//go:build !asr_server || !cgo

package controllers

import (
	"fmt"

	"xiaozhi/manager/backend/config"
)

func newEmbeddedSpeakerServiceClient(_ config.SpeakerServiceConfig) (speakerServiceClient, error) {
	return nil, fmt.Errorf("speaker_service.mode=embed 需要使用 -tags asr_server 且启用 CGO 编译")
}
