package local_asr_server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gorilla/websocket"
)

// Config 配置结构体
type Config struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	WsURL      string `json:"ws_url"`
	SampleRate int    `json:"sample_rate"`
	Timeout    int    `json:"timeout"`
}

// DefaultConfig 默认配置
var DefaultConfig = Config{
	Host:       "127.0.0.1",
	Port:       "9000",
	SampleRate: 16000,
	Timeout:    30,
}

// LocalAsrServer 实现 local_asr_server ASR 引擎
type LocalAsrServer struct {
	config Config
	dialer *websocket.Dialer
}

// ServerResponse asr_server WebSocket 返回的消息解包
type ServerResponse struct {
	Type      string `json:"type"`      // connection, final, error
	Message   string `json:"message"`   // 包含连接或错误消息
	Text      string `json:"text"`      // 识别出的文本 (type == "final")
	Timestamp int64  `json:"timestamp"` // 时间戳
	SessionID string `json:"session_id"`
}

// NewLocalAsrServer 创建新的 LocalAsrServer 实例
func NewLocalAsrServer(config Config) (*LocalAsrServer, error) {
	if config.Host == "" && config.WsURL == "" {
		config.Host = DefaultConfig.Host
	}
	if config.Port == "" && config.WsURL == "" {
		config.Port = DefaultConfig.Port
	}
	if config.SampleRate <= 0 {
		config.SampleRate = DefaultConfig.SampleRate
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultConfig.Timeout
	}

	return &LocalAsrServer{
		config: config,
		dialer: &websocket.Dialer{
			HandshakeTimeout: time.Duration(config.Timeout) * time.Second,
		},
	}, nil
}

func (s *LocalAsrServer) getWsURL() string {
	if s.config.WsURL != "" {
		return s.config.WsURL
	}
	host := s.config.Host
	port := s.config.Port
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") || strings.HasPrefix(host, "ws://") || strings.HasPrefix(host, "wss://") {
		parts := strings.Split(host, "://")
		if len(parts) == 2 {
			scheme := "ws"
			if parts[0] == "https" || parts[0] == "wss" {
				scheme = "wss"
			}
			return fmt.Sprintf("%s://%s:%s/ws", scheme, parts[1], port)
		}
	}
	return fmt.Sprintf("ws://%s:%s/ws", host, port)
}

// StreamingRecognize 实现流式识别
func (s *LocalAsrServer) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	wsURL := s.getWsURL()
	log.Debugf("local_asr_server 连接 WebSocket 地址: %s", wsURL)

	header := make(http.Header)
	conn, _, err := s.dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("local_asr_server 连接失败: %w", err)
	}

	resultChan := make(chan types.StreamingResult, 20)
	subCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	// Goroutine 1: 发送音频数据
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-subCtx.Done():
				return
			case samples, ok := <-audioStream:
				if !ok {
					// 音频发送完成，静默退出发送循环
					return
				}
				if len(samples) == 0 {
					continue
				}

				// 将 float32 样本转成 16-bit PCM 小端字节
				pcmBytes := make([]byte, len(samples)*2)
				for i, sample := range samples {
					if sample > 1.0 {
						sample = 1.0
					} else if sample < -1.0 {
						sample = -1.0
					}
					val := int16(math.Round(float64(sample * 32767.0)))
					binary.LittleEndian.PutUint16(pcmBytes[i*2:], uint16(val))
				}

				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.BinaryMessage, pcmBytes); err != nil {
					log.Warnf("local_asr_server 发送音频数据失败: %v", err)
					return
				}
			}
		}
	}()

	// Goroutine 2: 接收识别结果
	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
			cancel()
			close(resultChan)
			_ = conn.Close()
		}()

		for {
			_ = conn.SetReadDeadline(time.Now().Add(time.Duration(s.config.Timeout) * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) && subCtx.Err() == nil {
					log.Debugf("local_asr_server 读取消息完成或结束: %v", err)
				}
				return
			}

			var resp ServerResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				log.Warnf("local_asr_server 解析响应失败: %v, body: %s", err, string(message))
				continue
			}

			switch resp.Type {
			case "connection":
				log.Debugf("local_asr_server 握手成功, session_id=%s, msg=%s", resp.SessionID, resp.Message)
			case "final":
				if resp.Text != "" {
					select {
					case resultChan <- types.StreamingResult{
						Text:    resp.Text,
						IsFinal: true,
						AsrType: constants.AsrTypeLocalAsrServer,
					}:
						log.Infof("local_asr_server 识别出结果: %s", resp.Text)
					case <-subCtx.Done():
						return
					}
				}
			case "error":
				log.Errorf("local_asr_server 收到错误消息: %s", resp.Message)
			}
		}
	}()

	// 清理控制
	go func() {
		wg.Wait()
		_ = conn.Close()
	}()

	return resultChan, nil
}

// Process 一次性处理整段音频
func (s *LocalAsrServer) Process(pcmData []float32) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.Timeout)*time.Second)
	defer cancel()

	audioChan := make(chan []float32, 1)
	audioChan <- pcmData
	close(audioChan)

	resultChan, err := s.StreamingRecognize(ctx, audioChan)
	if err != nil {
		return "", err
	}

	var fullText string
	for res := range resultChan {
		if res.Error != nil {
			return fullText, res.Error
		}
		if res.Text != "" {
			fullText += res.Text
		}
	}
	return fullText, nil
}
