package eino_llm

import (
	"context"
	"encoding/base64"
	"fmt"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

func (p *EinoLLMProvider) ResponseWithVllm(ctx context.Context, file []byte, text string, mimeType string) (string, error) {
	log.Infof("[Eino-LLM] 开始进行VLLM请求 - MIMEType: %s, file length: %d", mimeType, len(file))

	// 将图片文件以base64编码，组装为data url
	base64Str := base64.StdEncoding.EncodeToString(file)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Str)

	msg := &schema.Message{
		Role: schema.User,
		MultiContent: []schema.ChatMessagePart{
			{
				Type: schema.ChatMessagePartTypeText,
				Text: text,
			},
			{
				Type: schema.ChatMessagePartTypeImageURL,
				ImageURL: &schema.ChatMessageImageURL{
					URL: dataURL,
				},
			},
		},
	}

	dialogue := []*schema.Message{
		&schema.Message{
			Role:    schema.System,
			Content: "你是一个专业的图片识别专家，请根据图片内容使用中文回答用户的问题。",
		},
		msg,
	}
	outputMsg, err := p.Generate(ctx, dialogue)
	if err != nil {
		log.Errorf("[Eino-VLLM] 调用视觉api请求处理失败 - %v", err)
		return "", fmt.Errorf("调用视觉api请求处理失败 - %v", err)
	}

	return outputMsg.Content, nil
}
