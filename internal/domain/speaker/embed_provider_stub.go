//go:build !asr_server || !cgo

package speaker

import (
	"context"
	"fmt"
)

// EmbedProvider 是未启用 embed 能力时的占位类型。
type EmbedProvider struct{}

// NewEmbedProvider 在未启用 asr_server tag 或未启用 cgo 时返回明确错误。
func NewEmbedProvider(config map[string]interface{}) (*EmbedProvider, error) {
	_ = config
	return nil, fmt.Errorf("speaker embed 需要使用 -tags asr_server 且启用 CGO 编译")
}

func (p *EmbedProvider) StartStreaming(ctx context.Context, sampleRate int, agentId string) error {
	_ = ctx
	_ = sampleRate
	_ = agentId
	return fmt.Errorf("speaker embed 需要使用 -tags asr_server 且启用 CGO 编译")
}

func (p *EmbedProvider) SendAudioChunk(ctx context.Context, audioData []float32) error {
	_ = ctx
	_ = audioData
	return fmt.Errorf("speaker embed 需要使用 -tags asr_server 且启用 CGO 编译")
}

func (p *EmbedProvider) FinishAndIdentify(ctx context.Context) (*IdentifyResult, error) {
	_ = ctx
	return nil, fmt.Errorf("speaker embed 需要使用 -tags asr_server 且启用 CGO 编译")
}

func (p *EmbedProvider) IsActive() bool {
	return false
}

func (p *EmbedProvider) Close() error {
	return nil
}
