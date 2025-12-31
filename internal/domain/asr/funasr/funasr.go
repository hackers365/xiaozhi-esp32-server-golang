package funasr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"

	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
)

// FunasrConfig 配置结构体
type FunasrConfig struct {
	Host           string // FunASR 服务主机地址
	Port           string // FunASR 服务端口
	Mode           string // 识别模式，如 "online"
	SampleRate     int    // 采样率
	ChunkSize      []int  // 分块大小
	ChunkInterval  int    // 分块间隔
	MaxConnections int    // 最大连接数
	Timeout        int    // 连接超时时间（秒）
	AutoEnd        bool   // 是否超时 xx ms自动结束，不依赖 isSpeaking为false
}

// DefaultConfig 默认配置
var DefaultConfig = FunasrConfig{
	Host:           "localhost",
	Port:           "10095",
	Mode:           "online",
	SampleRate:     audio.SampleRate,
	ChunkInterval:  10,
	ChunkSize:      []int{5, 10, 5},
	MaxConnections: 5,
	Timeout:        30,
}

// FunasrConnection 表示一个到FunASR服务的连接
type FunasrConnection struct {
	inUse    bool
	lastUsed time.Time
	writeMu  sync.Mutex // 添加写入锁
}

// Funasr 实现ASR接口
type Funasr struct {
	config    FunasrConfig
	pool      map[*websocket.Conn]*FunasrConnection
	poolMutex sync.Mutex
}

// FunasrRequest FunASR WebSocket请求结构体
type FunasrRequest struct {
	Mode          string `json:"mode,omitempty"`           // 识别模式，如 "online"
	ChunkSize     []int  `json:"chunk_size,omitempty"`     // 分块大小
	ChunkInterval int    `json:"chunk_interval,omitempty"` // 分块间隔
	AudioFs       int    `json:"audio_fs,omitempty"`       // 采样率
	WavName       string `json:"wav_name,omitempty"`       // 音频名称
	WavFormat     string `json:"wav_format,omitempty"`     // 音频格式
	IsSpeaking    bool   `json:"is_speaking"`              // 是否在说话
	Hotwords      string `json:"hotwords,omitempty"`       // 热词
	Itn           bool   `json:"itn,omitempty"`            // 是否进行文本规整
}

// FunasrResponse FunASR WebSocket响应结构体
type FunasrResponse struct {
	Text       string  `json:"text"`       // 识别的文本
	IsFinal    bool    `json:"is_final"`   // 是否为最终结果
	WavName    string  `json:"wav_name"`   // 音频名称
	TimeStamp  string  `json:"timestamp"`  // 时间戳
	Mode       string  `json:"mode"`       // 模式
	Confidence float64 `json:"confidence"` // 置信度
}

// NewFunasr 创建一个新的Funasr实例
func NewFunasr(config FunasrConfig) (*Funasr, error) {
	if config.Host == "" {
		config = DefaultConfig
	}

	f := &Funasr{
		config: config,
		pool:   make(map[*websocket.Conn]*FunasrConnection),
	}

	// 启动连接池清理协程
	go f.cleanupConnections()

	return f, nil
}

// createConnection 创建一个新的WebSocket连接
func (f *Funasr) createConnection() (*websocket.Conn, error) {
	url := fmt.Sprintf("ws://%s:%s/", f.config.Host, f.config.Port)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("连接到FunASR服务失败: %v", err)
	}
	return conn, nil
}

// removeConnection 移除无效连接
func (f *Funasr) removeConnection(conn *websocket.Conn) {
	f.poolMutex.Lock()
	defer f.poolMutex.Unlock()
	if _, ok := f.pool[conn]; ok {
		conn.Close()
		delete(f.pool, conn)
		log.Debugf("移除无效FunASR连接，当前连接数: %d", len(f.pool))
	}
}

// getConnection 从连接池获取一个连接
func (f *Funasr) getConnection() (*websocket.Conn, error) {
	f.poolMutex.Lock()
	defer f.poolMutex.Unlock()

	// 先查找可用的连接
	for conn, connInfo := range f.pool {
		if !connInfo.inUse {
			// 检查连接有效性，尝试Ping
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(1*time.Second)); err != nil {
				// 连接无效，移除
				go f.removeConnection(conn)
				continue
			}
			connInfo.inUse = true
			connInfo.lastUsed = time.Now()
			return conn, nil
		}
	}

	// 如果没有可用连接，且池未满，创建新连接
	if len(f.pool) < f.config.MaxConnections {
		conn, err := f.createConnection()
		if err != nil {
			return nil, err
		}
		f.pool[conn] = &FunasrConnection{
			inUse:    true,
			lastUsed: time.Now(),
		}
		log.Debugf("创建新的FunASR连接，当前连接数: %d", len(f.pool))
		return conn, nil
	}

	return nil, errors.New("连接池已满，无可用连接")
}

// releaseConnection 释放连接回池
func (f *Funasr) releaseConnection(conn *websocket.Conn) {
	f.poolMutex.Lock()
	defer f.poolMutex.Unlock()

	if connInfo, ok := f.pool[conn]; ok {
		connInfo.inUse = false
		connInfo.lastUsed = time.Now()
	}
}

// cleanupConnections 定期清理超时连接
func (f *Funasr) cleanupConnections() {
	ticker := time.NewTicker(time.Duration(f.config.Timeout) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		f.poolMutex.Lock()
		now := time.Now()
		for conn, connInfo := range f.pool {
			// 如果连接空闲超过超时时间，关闭并移除
			if !connInfo.inUse && now.Sub(connInfo.lastUsed) > time.Duration(f.config.Timeout)*time.Second {
				conn.Close()
				// 从池中移除
				delete(f.pool, conn)
				log.Debugf("关闭空闲连接，当前连接数: %d", len(f.pool))
			}
		}
		f.poolMutex.Unlock()
	}
}

// StreamingResult 流式识别结果
type StreamingResult struct {
	Text    string // 识别的文本
	IsFinal bool   // 是否为最终结果
}

// isTimeoutError 判断是否为超时错误
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为网络超时错误
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}

	// 检查错误消息中是否包含超时关键词
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "i/o timeout")
}

// isConnectionClosedError 判断是否为连接关闭错误
func isConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为 WebSocket 关闭错误
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
		return true
	}

	// 检查错误消息中是否包含连接关闭关键词
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "connection closed") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "use of closed network connection")
}

// writeMessage 安全地向 WebSocket 连接写入消息
func (f *Funasr) writeMessage(conn *websocket.Conn, messageType int, data []byte) error {
	if connInfo, ok := f.pool[conn]; ok {
		connInfo.writeMu.Lock()
		defer connInfo.writeMu.Unlock()
		return conn.WriteMessage(messageType, data)
	}
	return errors.New("connection not found in pool")
}

// StreamingRecognize 实现流式识别
// 从audioStream接收音频数据，通过resultChan返回结果
// 可以通过ctx控制识别过程的取消和超时
func (f *Funasr) StreamingRecognize(ctx context.Context, audioStream *schema.StreamReader[[]float32]) (chan types.StreamingResult, error) {
	// 获取一个连接
	conn, err := f.getConnection()
	if err != nil {
		return nil, err
	}

	subCtx, cancelFunc := context.WithCancel(ctx)

	// 发送初始消息
	firstMessage := FunasrRequest{
		Mode:          f.config.Mode,
		ChunkSize:     []int{5, 10, 5},
		ChunkInterval: f.config.ChunkInterval,
		AudioFs:       f.config.SampleRate,
		WavName:       "stream",
		WavFormat:     "pcm",
		IsSpeaking:    true,
		Hotwords:      "{\"阿里巴巴\":20,\"hello world\":40}",
		Itn:           true,
	}

	messageBytes, err := json.Marshal(firstMessage)
	if err != nil {
		f.releaseConnection(conn)
		return nil, fmt.Errorf("序列化初始消息失败: %v", err)
	}

	err = f.writeMessage(conn, websocket.TextMessage, messageBytes)
	if err != nil {
		f.releaseConnection(conn)
		return nil, fmt.Errorf("发送初始消息失败: %v", err)
	}

	// 创建结果通道，带缓冲避免阻塞
	resultChan := make(chan types.StreamingResult, 20)

	// 启动goroutine接收和发送数据
	go f.recvResult(subCtx, conn, resultChan)
	go f.forwardStreamAudio(subCtx, cancelFunc, conn, audioStream)

	return resultChan, nil
}

func (f *Funasr) recvResult(ctx context.Context, conn *websocket.Conn, resultChan chan types.StreamingResult) {
	defer func() {
		close(resultChan)
		f.releaseConnection(conn)
	}()

	for {
		select {
		case <-ctx.Done():
			// 上下文取消，退出goroutine
			log.Debugf("funasr recvResult 已取消: %v", ctx.Err())
			return
		default:
			// 继续正常处理
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Debugf("funasr recvResult 读取识别结果失败: %v", err)
			return
		}
		log.Debugf("funasr recvResult 读取识别结果: %v", string(message))

		var response FunasrResponse
		err = json.Unmarshal(message, &response)
		if err != nil {
			log.Debugf("funasr recvResult 解析识别结果失败: %v", err)
			continue
		}

		// 只有有文本时才发送结果
		/*if response.Text == "" {
			continue
		}*/

		// 发送识别结果
		select {
		case <-ctx.Done():
			// 上下文取消，退出goroutine
			log.Debugf("funasr recvResult 已取消: %v", ctx.Err())
			return
		case resultChan <- types.StreamingResult{
			Text:    response.Text,
			IsFinal: response.IsFinal,
		}:
		}
		/*if f.config.AutoEnd {
			log.Debugf("funasr recvResult autoend")
			return
		}*/
		// 结果发送成功
		// 如果是最终结果且输入已结束，则退出循环
		if response.IsFinal {
			log.Debugf("funasr recvResult isfinal")
			return
		}
	}
}

func (f *Funasr) forwardStreamAudio(ctx context.Context, cancelFunc context.CancelFunc, conn *websocket.Conn, audioStream *schema.StreamReader[[]float32]) {
	sendEndMsg := func() {
		// 发送终止消息
		endMessage := FunasrRequest{
			Mode:          f.config.Mode,
			ChunkInterval: f.config.ChunkInterval,
			ChunkSize:     []int{5, 10, 5},
			WavName:       "stream",
			IsSpeaking:    false,
		}
		endMessageBytes, _ := json.Marshal(endMessage)
		log.Debugf("funasr forwardStreamAudio 发送结束消息: %v", string(endMessageBytes))
		err := f.writeMessage(conn, websocket.TextMessage, endMessageBytes)
		if err != nil {
			log.Debugf("funasr forwardStreamAudio 发送结束消息失败: %v", err)
		}
		return
	}
	// 处理输入音频流
	for {
		select {
		case <-ctx.Done():
			// 上下文取消，发送结束消息并退出
			log.Debugf("funasr forwardStreamAudio 上下文已取消: %v", ctx.Err())
			cancelFunc() // 确保结束时取消上下文，通知接收goroutine
			sendEndMsg()
			return
		default:
		}
		pcmChunk, err := audioStream.Recv()
		if err != nil {
			if err == io.EOF {
				sendEndMsg()
				return
			}
			log.Debugf("funasr forwardStreamAudio 读取音频数据失败: %v", err)
			return
		}

		// 转换PCM数据为字节
		audioBytes := Float32SliceToBytes(pcmChunk)

		log.Debugf("funasr forwardStreamAudio 发送音频数据, pcmChunk len: %v, audioBytes len: %v", len(pcmChunk), len(audioBytes))

		// 发送音频数据
		err = f.writeMessage(conn, websocket.BinaryMessage, audioBytes)
		if err != nil {
			log.Debugf("funasr forwardStreamAudio 发送音频数据失败: %v", err)
			return
		}

	}
}

// Process 处理音频数据并返回识别结果
func (f *Funasr) Process(pcmData []float32) (string, error) {
	// 获取一个连接
	conn, err := f.getConnection()
	if err != nil {
		return "", err
	}
	defer f.releaseConnection(conn)

	audioBytes := Float32SliceToBytes(pcmData)

	// 发送初始消息
	firstMessage := FunasrRequest{
		Mode:          f.config.Mode,
		ChunkSize:     []int{5, 10, 5},
		ChunkInterval: f.config.ChunkInterval,
		AudioFs:       f.config.SampleRate,
		WavName:       "stream",
		WavFormat:     "pcm",
		IsSpeaking:    true,
		Hotwords:      "",
		Itn:           true,
	}

	messageBytes, err := json.Marshal(firstMessage)
	if err != nil {
		return "", fmt.Errorf("序列化初始消息失败: %v", err)
	}

	err = f.writeMessage(conn, websocket.TextMessage, messageBytes)
	if err != nil {
		return "", fmt.Errorf("发送初始消息失败: %v", err)
	}

	// 将音频数据按块发送
	chunkSize := int(audio.SampleRate * 0.1) // 每块大小约100ms的音频 (16000 * 0.1)
	for i := 0; i < len(audioBytes); i += chunkSize {
		end := i + chunkSize
		if end > len(audioBytes) {
			end = len(audioBytes)
		}
		chunk := audioBytes[i:end]

		err = f.writeMessage(conn, websocket.BinaryMessage, chunk)
		if err != nil {
			return "", fmt.Errorf("发送音频数据失败: %v", err)
		}
	}

	// 发送终止消息
	endMessage := FunasrRequest{
		IsSpeaking: false,
	}
	endMessageBytes, _ := json.Marshal(endMessage)
	err = f.writeMessage(conn, websocket.TextMessage, endMessageBytes)
	if err != nil {
		return "", fmt.Errorf("发送终止消息失败: %v", err)
	}

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(time.Duration(f.config.Timeout) * time.Second))

	// 读取结果
	var result string
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if isTimeoutError(err) {
				log.Debugf("funasr Process 读取结果超时: %v", err)
				f.removeConnection(conn) // 读取超时，移除连接
				return "", fmt.Errorf("读取结果超时: %v", err)
			}
			if isConnectionClosedError(err) {
				log.Debugf("funasr Process 读取结果连接已关闭: %v", err)
				f.removeConnection(conn) // 连接已关闭，移除连接
				return "", fmt.Errorf("连接已关闭: %v", err)
			}
			f.removeConnection(conn) // 读取失败时移除连接
			return "", fmt.Errorf("读取结果失败: %v", err)
		}

		var response FunasrResponse
		err = json.Unmarshal(message, &response)
		if err != nil {
			continue
		}

		// 检查是否为最终结果
		if response.IsFinal {
			result = response.Text
			break
		}
	}

	return result, nil
}

func Float32ToInt16(sample float32) int16 {
	// 限制在 [-1, 1]，避免溢出
	if sample > 1.0 {
		sample = 1.0
	} else if sample < -1.0 {
		sample = -1.0
	}
	return int16(sample * 32767)
}

func Float32SliceToBytes(samples []float32) []byte {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		i16 := Float32ToInt16(s)
		data[2*i] = byte(i16)
		data[2*i+1] = byte(i16 >> 8)
	}
	return data
}

/*
错误类型判断使用示例：

1. 超时错误判断：
   if isTimeoutError(err) {
       // 处理超时情况，可能需要重试或调整超时时间
       log.Warnf("操作超时: %v", err)
   }

2. 连接关闭错误判断：
   if isConnectionClosedError(err) {
       // 处理连接关闭情况，可能需要重新建立连接
       log.Warnf("连接已关闭: %v", err)
   }

3. 综合错误处理：
   _, message, err := conn.ReadMessage()
   if err != nil {
       if isTimeoutError(err) {
           // 超时：可能是网络延迟或服务器响应慢
           // 建议：调整超时时间或重试
       } else if isConnectionClosedError(err) {
           // 连接关闭：可能是服务器主动断开或网络中断
           // 建议：重新建立连接
       } else {
           // 其他错误：可能是协议错误或数据格式错误
           // 建议：检查数据格式或协议实现
       }
   }

常见错误类型：
- 超时错误：i/o timeout, context deadline exceeded
- 连接关闭：connection closed, broken pipe, connection reset
- WebSocket关闭：close 1000 (normal), close 1001 (going away)
*/
