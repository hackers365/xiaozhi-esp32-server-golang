//go:build !asr_server || !cgo

package asr

import "fmt"

// NewAsrEmbedAdapter 在未启用 asr_server tag 或未启用 cgo 时返回明确错误。
func NewAsrEmbedAdapter(config map[string]interface{}) (AsrProvider, error) {
	return nil, fmt.Errorf("embed 需要使用 -tags asr_server 且启用 CGO 编译")
}

// InitAsrServerEmbed 在未启用 asr_server 编译时返回明确错误。
func InitAsrServerEmbed(configPath string) error {
	return fmt.Errorf("embed 需要使用 -tags asr_server 且启用 CGO 编译")
}

// IsAsrServerEmbedInitialized 在未启用 asr_server 编译时恒为 false。
func IsAsrServerEmbedInitialized() bool {
	return false
}

// RequireAsrServerEmbed 在未启用 asr_server 编译时返回明确错误。
func RequireAsrServerEmbed(configPath string) error {
	return InitAsrServerEmbed(configPath)
}
