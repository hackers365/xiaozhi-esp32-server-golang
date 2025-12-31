package llm

import (
	"fmt"

	"xiaozhi-esp32-server-golang/constants"
	. "xiaozhi-esp32-server-golang/internal/domain/llm/common"
	"xiaozhi-esp32-server-golang/internal/domain/llm/eino_llm"
)

// LLMFactory 大语言模型工厂接口
// 用于创建不同类型的LLM提供者
type LLMFactory interface {
	// CreateProvider 根据配置创建LLM提供者
	CreateProvider(config map[string]interface{}) (LLMProvider, error)
}

// GetLLMProvider 创建LLM提供者
// 统一使用EinoLLMProvider处理所有类型
func GetLLMProvider(providerName string, config map[string]interface{}) (LLMProvider, error) {
	llmType := config["type"].(string)
	switch llmType {
	case constants.LlmTypeOpenai, constants.LlmTypeOllama, constants.LlmTypeEinoLLM, constants.LlmTypeEino:
		// 统一使用 EinoLLMProvider 处理所有类型
		provider, err := eino_llm.NewEinoLLMProvider(config)
		if err != nil {
			return nil, fmt.Errorf("创建Eino LLM提供者失败: %v", err)
		}
		return provider, nil
	}
	return nil, fmt.Errorf("不支持的LLM提供者: %s", llmType)
}

// Config LLM配置结构
type Config struct {
	ModelName  string                 `json:"model_name"`
	APIKey     string                 `json:"api_key"`
	BaseURL    string                 `json:"base_url"`
	MaxTokens  int                    `json:"max_tokens"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}
