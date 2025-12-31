package common

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// 请求与响应结构体
// Message 表示对话消息

// 响应类型常量
const (
	ResponseTypeContent   = "content"
	ResponseTypeToolCalls = "tool_calls"
)

type LLMResponseStruct struct {
	Text      string            `json:"text,omitempty"`
	IsStart   bool              `json:"is_start"`
	IsEnd     bool              `json:"is_end"`
	ToolCalls []schema.ToolCall `json:"tool_calls,omitempty"`
}

// LLMProvider 大语言模型提供者接口
// 所有LLM实现必须遵循此接口，使用Eino原生类型
type LLMProvider interface {
	//实现chatModel的Generate
	Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error)
	//实现chatModel的Stream
	Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error)

	WithTools(tools []*schema.ToolInfo) (LLMProvider, error)

	ResponseWithVllm(ctx context.Context, file []byte, text string, mimeType string) (string, error)

	// GetModelInfo 获取模型信息
	// 返回模型名称和其他元数据
	GetModelInfo() map[string]interface{}
}
