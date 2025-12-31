package doubao

import (
	"context"
	"fmt"
	"io"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao/client"
	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao/response"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
)

// DoubaoV2ASR 豆包ASR实现
type DoubaoV2ASR struct {
	config      DoubaoV2Config
	isStreaming bool
	reqID       string
	connectID   string

	// 流式识别相关字段
	result      string
	err         error
	sendDataCnt int
	c           *client.AsrWsClient
}

// NewDoubaoV2ASR 创建一个新的豆包ASR实例
func NewDoubaoV2ASR(config DoubaoV2Config) (*DoubaoV2ASR, error) {
	log.Info("创建豆包ASR实例")
	log.Info(fmt.Sprintf("配置: %+v", config))

	if config.AppID == "" {
		log.Error("缺少appid配置")
		return nil, fmt.Errorf("缺少appid配置")
	}
	if config.AccessToken == "" {
		log.Error("缺少access_token配置")
		return nil, fmt.Errorf("缺少access_token配置")
	}

	// 使用默认配置填充缺失的字段
	if config.WsURL == "" {
		config.WsURL = DefaultConfig.WsURL
	}
	if config.ModelName == "" {
		config.ModelName = DefaultConfig.ModelName
	}
	if config.EndWindowSize == 0 {
		config.EndWindowSize = DefaultConfig.EndWindowSize
	}
	if config.ChunkDuration == 0 {
		config.ChunkDuration = DefaultConfig.ChunkDuration
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultConfig.Timeout
	}

	connectID := fmt.Sprintf("%d", time.Now().UnixNano())

	return &DoubaoV2ASR{
		config:    config,
		connectID: connectID,
	}, nil
}

// StreamingRecognize 实现流式识别接口
func (d *DoubaoV2ASR) StreamingRecognize(ctx context.Context, audioStream *schema.StreamReader[[]float32]) (chan types.StreamingResult, error) {
	// 建立连接
	d.c = client.NewAsrWsClient(d.config.WsURL, d.config.AppID, d.config.AccessToken)

	// 豆包返回的识别结果
	doubaoResultChan := make(chan *response.AsrResponse, 10)
	//程序内部的结果通道
	resultChan := make(chan types.StreamingResult, 10)

	err := d.c.CreateConnection(ctx)
	if err != nil {
		log.Errorf("doubao asr failed to create connection: %v", err)
		return nil, fmt.Errorf("create connection err: %w", err)
	}
	err = d.c.SendFullClientRequest()
	if err != nil {
		log.Errorf("doubao asr failed to send full request: %v", err)
		return nil, fmt.Errorf("send full request err: %w", err)
	}

	go func() {
		audioStreamChan := make(chan []float32, 30)
		go func() {
			defer close(audioStreamChan)
			for {
				pcmChunk, err := audioStream.Recv()
				if err != nil {
					if err == io.EOF {
						return
					}
					return
				}
				select {
				case <-ctx.Done():
					return
				case audioStreamChan <- pcmChunk:
				}
			}
		}()
		err = d.c.StartAudioStream(ctx, audioStreamChan, doubaoResultChan)
	}()

	// 启动音频发送goroutine
	//go d.forwardStreamAudio(ctx, audioStream, resultChan)

	// 启动结果接收goroutine
	go d.receiveStreamResults(ctx, resultChan, doubaoResultChan)

	return resultChan, nil
}

// receiveStreamResults 接收流式识别结果
func (d *DoubaoV2ASR) receiveStreamResults(ctx context.Context, resultChan chan types.StreamingResult, asrResponseChan chan *response.AsrResponse) {
	defer func() {
		close(resultChan)
		d.c.Close()
	}()
	for {
		select {
		case <-ctx.Done():
			log.Debugf("receiveStreamResults 上下文已取消")
			return
		case result, ok := <-asrResponseChan:
			if !ok {
				log.Debugf("receiveStreamResults asrResponseChan 已关闭")
				return
			}
			if result.Code != 0 {
				resultChan <- types.StreamingResult{
					Text:    "",
					IsFinal: true,
					Error:   fmt.Errorf("asr response code: %d", result.Code),
				}
				return
			}
			if result.IsLastPackage {
				resultChan <- types.StreamingResult{
					Text:    result.PayloadMsg.Result.Text,
					IsFinal: true,
				}
				return
			}
		}
	}
}

// Reset 重置ASR状态
func (d *DoubaoV2ASR) Reset() error {

	log.Info("ASR状态已重置")
	return nil
}
