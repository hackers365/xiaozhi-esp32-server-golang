package stream_sentence

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	. "xiaozhi-esp32-server-golang/internal/util/sentence"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

type LLMResponseStruct struct {
	Text  string
	IsEnd bool
}

// HandleLLMWithContextAndTools 使用上下文控制来处理LLM响应（兼容带工具和不带工具）
func HandleLLMWithContextAndTools(ctx context.Context, input *schema.StreamReader[*schema.Message]) (*schema.StreamReader[*schema.Message], error) {

	outputStreamerReader, outputStreamWriter := schema.Pipe[*schema.Message](10)

	startTs := time.Now().UnixMilli()
	var firstFrame bool
	fullText := ""
	var buffer bytes.Buffer // 用于累积接收到的内容
	isFirst := true

	go func() {
		defer func() {
			log.Debugf("llm fullText: %s", fullText)
			outputStreamWriter.Close()
		}()

		for {
			select {
			case <-ctx.Done():
				log.Infof("上下文已取消，停止LLM响应处理: %v, context done, exit", ctx.Err())
				return
			default:
			}
			//case message, ok := <-msgChan:
			message, err := input.Recv()
			if err != nil {
				var retstring string
				if err == io.EOF {
					err = nil
					remaining := buffer.String()
					if remaining != "" {
						log.Infof("处理剩余内容: %s", remaining)
						fullText += remaining
						retstring = remaining
					}
				}
				outputStreamWriter.Send(&schema.Message{
					Role:    schema.Assistant,
					Content: retstring,
				}, err)
				return
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
								outputStreamWriter.Send(&schema.Message{
									Role:    schema.Assistant,
									Content: sentence,
								}, nil)

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
				outputStreamWriter.Send(&schema.Message{
					Role:      schema.Assistant,
					ToolCalls: message.ToolCalls,
				}, nil)
			}
		}
	}()
	return outputStreamerReader, nil
}
