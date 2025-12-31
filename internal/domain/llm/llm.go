package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"
	"xiaozhi-esp32-server-golang/internal/domain/llm/common"

	. "xiaozhi-esp32-server-golang/internal/util/sentence"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

// HandleLLMWithContextAndTools 使用上下文控制来处理LLM响应（兼容带工具和不带工具）
func HandleLLMWithContextAndTools(ctx context.Context, llmProvider common.LLMProvider, dialogue []*schema.Message, tools []*schema.ToolInfo, sessionID string) (chan common.LLMResponseStruct, error) {

	newLlmProvider, err := llmProvider.WithTools(tools)
	if err != nil {
		log.Errorf("WithTools 失败: %v", err)
		return nil, err
	}
	llmProvider = newLlmProvider

	llmStreamReader, err := llmProvider.Stream(ctx, dialogue)
	if err != nil {
		log.Errorf("Stream 失败: %v", err)
		return nil, err
	}

	sentenceChannel := make(chan common.LLMResponseStruct, 2)
	startTs := time.Now().UnixMilli()
	var firstFrame bool
	fullText := ""
	var buffer bytes.Buffer // 用于累积接收到的内容
	isFirst := true

	go func() {
		defer func() {
			log.Debugf("full Response with %d tools, fullText: %s", len(tools), fullText)
			close(sentenceChannel)
		}()
		for {
			select {
			case <-ctx.Done():
				log.Infof("上下文已取消，停止LLM响应处理: %v, context done, exit", ctx.Err())
				return
			default:
			}
			message, err := llmStreamReader.Recv()
			if err != nil {
				if err == io.EOF {
					remaining := buffer.String()
					if remaining != "" {
						log.Infof("处理剩余内容: %s", remaining)
						fullText += remaining
						select {
						case <-ctx.Done():
							log.Infof("上下文已取消，停止LLM响应处理: %v, context done, exit", ctx.Err())
							return
						case sentenceChannel <- common.LLMResponseStruct{
							Text:  remaining,
							IsEnd: true,
						}:
						}

					} else {
						select {
						case <-ctx.Done():
							log.Infof("上下文已取消，停止LLM响应处理: %v, context done, exit", ctx.Err())
							return
						case sentenceChannel <- common.LLMResponseStruct{
							Text:  "",
							IsEnd: true,
						}:
						}
					}
					return
				}
			}
			if message == nil {
				break
			}
			byteMessage, _ := json.Marshal(message)
			log.Infof("收到message: %s", string(byteMessage))
			if message.Content != "" {
				fullText += message.Content
				buffer.WriteString(message.Content)
				if ContainsSentenceSeparator(message.Content, isFirst) {
					sentences, remaining := ExtractSmartSentences(buffer.String(), 2, 100, isFirst)
					if len(sentences) > 0 {
						for _, sentence := range sentences {
							if sentence != "" {
								if !firstFrame {
									firstFrame = true
									log.Infof("耗时统计: llm工具首句: %d ms", time.Now().UnixMilli()-startTs)
								}
								log.Infof("处理完整句子: %s", sentence)
								select {
								case <-ctx.Done():
									log.Infof("上下文已取消，停止LLM响应处理: %v, context done, exit", ctx.Err())
									return
								case sentenceChannel <- common.LLMResponseStruct{
									Text:    sentence,
									IsStart: isFirst,
									IsEnd:   false,
								}:
								}

								if isFirst {
									isFirst = false
								}
							}
						}
					}
					buffer.Reset()
					buffer.WriteString(remaining)
					if isFirst {
						isFirst = false
					}
				}
			}
			// 工具调用响应（假设 ToolCalls 字段）
			if message.ToolCalls != nil && len(message.ToolCalls) > 0 {
				log.Infof("处理工具调用: %+v", message.ToolCalls)
				select {
				case <-ctx.Done():
					log.Infof("上下文已取消，停止LLM响应处理: %v, context done, exit", ctx.Err())
					return
				case sentenceChannel <- common.LLMResponseStruct{
					ToolCalls: message.ToolCalls,
					IsStart:   isFirst,
					IsEnd:     false,
				}:
				}
			}
		}

	}()
	return sentenceChannel, nil
}

// ConvertMCPToolsToEinoTools 将MCP工具转换为Eino ToolInfo格式
func ConvertMCPToolsToEinoTools(ctx context.Context, mcpTools map[string]interface{}) ([]*schema.ToolInfo, error) {
	var einoTools []*schema.ToolInfo

	for toolName, mcpTool := range mcpTools {
		// 尝试获取工具信息
		if invokableTool, ok := mcpTool.(interface {
			Info(context.Context) (*schema.ToolInfo, error)
		}); ok {
			toolInfo, err := invokableTool.Info(ctx)
			if err != nil {
				log.Errorf("获取工具 %s 信息失败: %v", toolName, err)
				continue
			}
			einoTools = append(einoTools, toolInfo)
		} else {
			log.Warnf("工具 %s 不支持Info接口，跳过转换", toolName)
		}
	}

	log.Infof("成功转换了 %d 个MCP工具为Eino工具", len(einoTools))
	return einoTools, nil
}
