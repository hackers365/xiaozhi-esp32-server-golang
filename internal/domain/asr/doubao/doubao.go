package doubao

import (
	"context"
	"fmt"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao/client"
	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao/response"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"
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
	if config.ResourceID == "" {
		config.ResourceID = DefaultConfig.ResourceID
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
// 注意：连接将在收到第一个音频包时延迟建立，避免因VAD延迟导致服务端超时
func (d *DoubaoV2ASR) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	// 创建客户端实例（不立即建立连接）
	d.c = client.NewAsrWsClient(d.config.WsURL, d.config.AppID, d.config.AccessToken, d.config.ResourceID)

	// 豆包返回的识别结果
	doubaoResultChan := make(chan *response.AsrResponse, 10)
	//程序内部的结果通道
	resultChan := make(chan types.StreamingResult, 10)

	// 启动音频流处理（连接将在第一个音频包到达时建立）
	go func() {
		defer close(doubaoResultChan)
		if err := d.c.StartAudioStream(ctx, audioStream, doubaoResultChan); err != nil {
			payload := &response.AsrResponsePayload{}
			payload.Error = err.Error()
			select {
			case <-ctx.Done():
			case doubaoResultChan <- &response.AsrResponse{
				Code:          -1,
				IsLastPackage: true,
				PayloadMsg:    payload,
			}:
			}
		}
	}()

	// 启动结果接收goroutine
	go d.receiveStreamResults(ctx, resultChan, doubaoResultChan)

	return resultChan, nil
}

// receiveStreamResults 接收流式识别结果
func (d *DoubaoV2ASR) receiveStreamResults(ctx context.Context, resultChan chan types.StreamingResult, asrResponseChan chan *response.AsrResponse) {
	defer func() {
		close(resultChan)
		if d.c != nil {
			d.c.Close()
		}
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
				errMsg := fmt.Sprintf("asr response code: %d", result.Code)
				if result.PayloadMsg != nil && result.PayloadMsg.Error != "" {
					errMsg = result.PayloadMsg.Error
				}
				// 使用 select 避免向已关闭的 channel 发送（如果 ctx 已取消，优先选择 ctx.Done()）
				select {
				case <-ctx.Done():
					log.Debugf("receiveStreamResults 发送错误结果时上下文已取消，跳过发送")
					return
				case resultChan <- types.StreamingResult{
					Text:    "",
					IsFinal: true,
					Error:   fmt.Errorf("%s", errMsg),
				}:
				}
				return
			}
			if result.IsLastPackage {
				// 处理最终结果（包括静音情况的空结果），使用 select 避免向已关闭的 channel 发送
				select {
				case <-ctx.Done():
					log.Debugf("receiveStreamResults 发送最终结果时上下文已取消，跳过发送")
					return
				case resultChan <- types.StreamingResult{
					Text:    result.PayloadMsg.Result.Text,
					IsFinal: true,
				}:
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

// Close 关闭资源，释放连接等
func (d *DoubaoV2ASR) Close() error {
	if d.c != nil {
		return d.c.Close()
	}
	return nil
}

// IsValid 检查资源是否有效
func (d *DoubaoV2ASR) IsValid() bool {
	return d != nil
}
