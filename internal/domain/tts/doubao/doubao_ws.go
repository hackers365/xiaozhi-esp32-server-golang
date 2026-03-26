package doubao

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gorilla/websocket"
)

// 枚举消息类型
var (
	enumMessageType = map[byte]string{
		11: "audio-only server response",
		12: "frontend server response",
		15: "error message from server",
	}
	enumMessageTypeSpecificFlags = map[byte]string{
		0: "no sequence number",
		1: "sequence number > 0",
		2: "last message from server (seq < 0)",
		3: "sequence number < 0",
	}
	enumMessageSerializationMethods = map[byte]string{
		0:  "no serialization",
		1:  "JSON",
		15: "custom type",
	}
	enumMessageCompression = map[byte]string{
		0:  "no compression",
		1:  "gzip",
		15: "custom compression method",
	}
)

// 常量定义
const (
	optQuery   string = "query"  // 一次性合成
	optSubmit  string = "submit" // 流式合成
	wsScheme   string = "wss"
	wsPath     string = "/api/v1/tts/ws_binary"
	headerSize int    = 4 // 默认头部大小(字节)
)

// 默认二进制协议头
// version: b0001 (4 bits)
// header size: b0001 (4 bits)
// message type: b0001 (Full client request) (4bits)
// message type specific flags: b0000 (none) (4bits)
// message serialization method: b0001 (JSON) (4 bits)
// message compression: b0001 (gzip) (4bits)
// reserved data: 0x00 (1 byte)
var defaultHeader = []byte{0x11, 0x10, 0x11, 0x00}

// 全局WebSocket Dialer，配置更大的缓冲区以避免slice bounds out of range错误
var wsDialer = websocket.Dialer{
	ReadBufferSize:   16384, // 16KB 读取缓冲区
	WriteBufferSize:  16384, // 16KB 写入缓冲区
	HandshakeTimeout: 45 * time.Second,
}

// 合成响应结构
type synResp struct {
	Audio  []byte
	IsLast bool
}

type serverError struct {
	code    int32
	message string
}

func (e serverError) Error() string {
	return fmt.Sprintf("服务端错误 (代码: %d): %s", e.code, e.message)
}

// DoubaoWSProvider 读伴WebSocket TTS提供者
type DoubaoWSProvider struct {
	AppID       string
	AccessToken string
	Cluster     string
	Voice       string
	WSHost      string
	WSURL       *url.URL
	Header      http.Header
	UseStream   bool // 是否使用流式合成
	// 音频片段处理回调函数，仅在流式模式下使用
	OnAudioChunk func(chunkData []byte, isLast bool) error

	// 连接管理
	conn      *websocket.Conn
	connMutex sync.RWMutex
	// 发送锁，确保同一时间只有一个请求在使用连接
	sendMutex sync.Mutex
}

// NewDoubaoWSProvider 创建新的读伴WebSocket TTS提供者
func NewDoubaoWSProvider(config map[string]interface{}) *DoubaoWSProvider {
	appID, _ := config["appid"].(string)
	accessToken, _ := config["access_token"].(string)
	cluster, _ := config["cluster"].(string)
	voice, _ := config["voice"].(string)
	wsHost, _ := config["ws_host"].(string)
	useStream, _ := config["use_stream"].(bool)

	// 如果没有指定WebSocket主机，使用默认值
	if wsHost == "" {
		wsHost = "openspeech.bytedance.com"
	}

	// 构建WebSocket URL
	wsURL := url.URL{Scheme: wsScheme, Host: wsHost, Path: wsPath}

	// 检查令牌
	if accessToken == "" {
		log.Error("TTS WebSocket 访问令牌不能为空")
	}

	// 创建HTTP头
	header := http.Header{}
	header.Add("Authorization", fmt.Sprintf("Bearer;%s", accessToken))

	return &DoubaoWSProvider{
		AppID:       appID,
		AccessToken: accessToken,
		Cluster:     cluster,
		Voice:       voice,
		WSHost:      wsHost,
		WSURL:       &wsURL,
		Header:      header,
		UseStream:   useStream,
	}
}

func (p *DoubaoWSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	return nil, nil
}

// TextToSpeech 将文本转换为语音，返回音频帧数据和错误
func (p *DoubaoWSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputOpusChan chan []byte, err error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	if !hasReadableText(text) {
		log.Warnf("TTS 文本无可读字符，跳过请求")
		return nil, nil
	}

	var operation string
	if p.UseStream {
		operation = optSubmit // 流式合成
	} else {
		operation = optQuery // 一次性合成
	}

	startTs := time.Now().UnixMilli()

	// 准备请求数据
	input := p.setupInput(text, p.Voice, operation, sampleRate, channels, frameDuration)

	// 使用发送锁保护，确保同一时间只有一个请求在使用连接
	p.sendMutex.Lock()
	// 注意：不在函数返回时释放锁，而是在 goroutine 完成时释放

	// 获取连接（复用或创建）
	conn, err := p.getConnection(ctx)
	if err != nil {
		p.sendMutex.Unlock() // 获取连接失败时立即释放锁
		return nil, fmt.Errorf("获取WebSocket连接失败: %v", err)
	}

	// 压缩输入
	compressedInput := gzipCompress(input)
	payloadSize := len(compressedInput)

	// 准备payload大小数据
	payloadArr := make([]byte, 4)
	binary.BigEndian.PutUint32(payloadArr, uint32(payloadSize))

	// 组装完整的请求
	clientRequest := make([]byte, len(defaultHeader))
	copy(clientRequest, defaultHeader)
	clientRequest = append(clientRequest, payloadArr...)
	clientRequest = append(clientRequest, compressedInput...)

	// 发送请求（使用受保护的写入方法）
	err = p.writeMessage(conn, websocket.BinaryMessage, clientRequest)
	if err != nil {
		// 发送失败，清空连接，下次使用时自动重连
		log.Errorf("发送WebSocket消息失败: %v，清空连接", err)
		p.clearConnection()
		p.sendMutex.Unlock() // 发送失败时立即释放锁
		return nil, fmt.Errorf("发送WebSocket消息失败: %v", err)
	}

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	pipeReader, pipeWriter := io.Pipe()

	outputOpusChan = make(chan []byte, 1000)

	// 启动解码器 goroutine
	go func() {
		mp3Decoder, err := util.CreateAudioDecoder(ctx, pipeReader, outputOpusChan, frameDuration, "mp3")
		if err != nil {
			log.Errorf("创建MP3解码器失败: %v", err)
			close(outputOpusChan)
			return
		}
		err = mp3Decoder.Run(startTs)
		if err != nil {
			log.Errorf("MP3解码器运行失败: %v", err)
			return
		}
	}()

	// 使用 WaitGroup 等待读取 goroutine 完成
	var wg sync.WaitGroup
	wg.Add(1)

	// 启动读取 goroutine；锁在此 goroutine 内统一由 defer 释放，确保无论正常结束、错误或 panic 都会释放
	go func() {
		defer wg.Done()
		defer p.sendMutex.Unlock()
		defer func() {
			pipeWriter.Close()
		}()
		// 流式合成
		chunkCount := 0
		retryCount := 0
		maxRetry := 1
		//var allAudio []byte
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if chunkCount == 0 && retryCount < maxRetry {
					retryCount++
					log.Warnf("读取WebSocket消息失败(无音频)，尝试重连重试: %v", err)
					p.clearConnection()
					conn, err = p.getConnection(ctx)
					if err != nil {
						log.Errorf("重连WebSocket失败: %v", err)
						return
					}
					if err = p.writeMessage(conn, websocket.BinaryMessage, clientRequest); err != nil {
						log.Errorf("重发WebSocket消息失败: %v，清空连接", err)
						p.clearConnection()
						return
					}
					conn.SetReadDeadline(time.Now().Add(10 * time.Second))
					continue
				}
				log.Errorf("读取WebSocket消息失败: %v，清空连接", err)
				// 连接断开，清空连接，下次使用时自动重连
				p.clearConnection()
				return
			}

			resp, err := parseResponse(message)
			if err != nil {
				var srvErr serverError
				if errors.As(err, &srvErr) && srvErr.code == 3011 {
					log.Warnf("服务端返回不可读文本错误(代码: %d): %s", srvErr.code, srvErr.message)
					return
				}
				if chunkCount == 0 && retryCount < maxRetry {
					retryCount++
					log.Warnf("解析响应失败(无音频)，尝试重连重试: %v", err)
					p.clearConnection()
					conn, err = p.getConnection(ctx)
					if err != nil {
						log.Errorf("重连WebSocket失败: %v", err)
						return
					}
					if err = p.writeMessage(conn, websocket.BinaryMessage, clientRequest); err != nil {
						log.Errorf("重发WebSocket消息失败: %v，清空连接", err)
						p.clearConnection()
						return
					}
					conn.SetReadDeadline(time.Now().Add(10 * time.Second))
					continue
				}
				// 解析失败时必须清连接，避免当前连接残留响应污染下一次TTS请求
				log.Errorf("解析响应失败: %v，清空连接", err)
				p.clearConnection()
				return
			}

			select {
			case <-ctx.Done():
				if resp.IsLast {
					log.Debugf("DoubaoWs TextToSpeechStream context done, already read all data, exit")
					return
				} else {
					log.Debugf("DoubaoWs TextToSpeechStream context done, need to read more data, continue")
					continue
				}
			default:
			}

			if len(resp.Audio) > 0 {
				chunkCount++
				// 存储用于最终返回
				//allAudio = append(allAudio, resp.Audio...)
				if _, err := pipeWriter.Write(resp.Audio); err != nil {
					log.Errorf("写入音频管道失败: %v，清空连接", err)
					p.clearConnection()
					return
				}
			}

			if resp.IsLast {
				log.Debugf("收到最后一个音频片段，共%d个片段", chunkCount)
				//将allAudio写到文件中
				//saveAudioToTmp(allAudio, "mp3")
				return
			}

			// 重置读取超时
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		}
	}()

	return outputOpusChan, nil
}

// GetVoiceInfo 获取语音信息
func (p *DoubaoWSProvider) GetVoiceInfo() map[string]interface{} {
	return map[string]interface{}{
		"voice": p.Voice,
		"type":  "doubao_ws",
	}
}

// getConnection 获取连接，如果不存在则创建
func (p *DoubaoWSProvider) getConnection(ctx context.Context) (*websocket.Conn, error) {
	// 先尝试读取现有连接
	p.connMutex.RLock()
	conn := p.conn
	p.connMutex.RUnlock()

	if conn != nil {
		return conn, nil
	}

	// 需要创建新连接
	p.connMutex.Lock()
	defer p.connMutex.Unlock()

	// 双重检查，可能其他 goroutine 已经创建了连接
	if p.conn != nil {
		return p.conn, nil
	}

	// 创建新连接
	conn, _, err := wsDialer.DialContext(ctx, p.WSURL.String(), p.Header)
	if err != nil {
		return nil, fmt.Errorf("WebSocket连接失败: %v", err)
	}

	// 设置消息读取限制，防止过大的消息
	conn.SetReadLimit(1024 * 1024) // 1MB 最大消息大小

	// 设置保持连接
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(1*time.Second))
	})

	p.conn = conn
	log.Infof("WebSocket 连接已建立")
	return conn, nil
}

// clearConnection 清空连接（用于断线重连）
func (p *DoubaoWSProvider) clearConnection() {
	p.connMutex.Lock()
	defer p.connMutex.Unlock()

	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
		log.Infof("WebSocket 连接已清空，等待下次重连")
	}
}

// writeMessage 安全地向 WebSocket 连接写入消息
func (p *DoubaoWSProvider) writeMessage(conn *websocket.Conn, messageType int, data []byte) error {
	// 使用读锁保护连接写入操作，防止并发写入导致数据混乱
	p.connMutex.RLock()
	defer p.connMutex.RUnlock()

	// 检查连接是否有效
	if conn == nil {
		return fmt.Errorf("连接已关闭")
	}

	return conn.WriteMessage(messageType, data)
}

// 设置请求输入
func (p *DoubaoWSProvider) setupInput(text, voiceType, opt string, sampleRate int, channels int, frameDuration int) []byte {
	// 生成请求ID
	reqID := generateUUID()

	// 构建请求参数
	params := make(map[string]map[string]interface{})

	// 应用信息
	params["app"] = make(map[string]interface{})
	params["app"]["appid"] = p.AppID
	params["app"]["token"] = p.AccessToken
	params["app"]["cluster"] = p.Cluster

	// 用户信息
	params["user"] = make(map[string]interface{})
	params["user"]["uid"] = "uid"

	// 音频参数
	params["audio"] = make(map[string]interface{})
	params["audio"]["voice_type"] = voiceType
	params["audio"]["encoding"] = "mp3"
	params["audio"]["rate"] = sampleRate
	params["audio"]["speed_ratio"] = 1.0
	params["audio"]["volume_ratio"] = 1.0
	params["audio"]["pitch_ratio"] = 1.0

	// 请求信息
	params["request"] = make(map[string]interface{})
	params["request"]["reqid"] = reqID
	params["request"]["text"] = text
	params["request"]["text_type"] = "plain"
	params["request"]["operation"] = opt

	// 序列化为JSON
	resBytes, _ := json.Marshal(params)
	return resBytes
}

func hasReadableText(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

// gzip压缩
func gzipCompress(input []byte) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(input)
	w.Close()
	return b.Bytes()
}

// gzip解压缩
func gzipDecompress(input []byte) []byte {
	b := bytes.NewBuffer(input)
	r, _ := gzip.NewReader(b)
	out, _ := io.ReadAll(r)
	r.Close()
	return out
}

// 解析WebSocket响应
func parseResponse(res []byte) (resp synResp, err error) {
	// 解析头部
	protoVersion := res[0] >> 4
	headSizeByte := res[0] & 0x0f
	messageType := res[1] >> 4
	messageTypeSpecificFlags := res[1] & 0x0f
	serializationMethod := res[2] >> 4
	messageCompression := res[2] & 0x0f

	_ = protoVersion
	_ = serializationMethod
	/*
		log.Debugf("协议版本: %x - 版本 %d\n", protoVersion, protoVersion)
		log.Debugf("头部大小: %x - %d 字节\n", headSizeByte, headSizeByte*4)
		log.Debugf("消息类型: %x - %s\n", messageType, enumMessageType[messageType])
		log.Debugf("消息类型特定标志: %x - %s\n", messageTypeSpecificFlags, enumMessageTypeSpecificFlags[messageTypeSpecificFlags])
		log.Debugf("消息序列化方法: %x - %s\n", serializationMethod, enumMessageSerializationMethods[serializationMethod])
		log.Debugf("消息压缩: %x - %s\n", messageCompression, enumMessageCompression[messageCompression])
	*/

	// 计算头部大小（以字节为单位）
	headSizeBytes := int(headSizeByte) * 4

	// 分离payload
	payload := res[headSizeBytes:]

	// 根据消息类型处理
	if messageType == 0xb { // audio-only server response (11)
		// 检查是否有序列号
		if messageTypeSpecificFlags == 0 {
			// 无序列号，空payload
			return
		} else {
			// 有序列号，提取payload
			sequenceNumber := int32(binary.BigEndian.Uint32(payload[0:4]))
			payloadSize := int32(binary.BigEndian.Uint32(payload[4:8]))
			payload = payload[8:]

			_ = payloadSize
			//log.Debugf("序列号: %d", sequenceNumber)
			//log.Debugf("Payload大小: %d", payloadSize)

			resp.Audio = append(resp.Audio, payload...)

			// 检查是否为最后一个包
			if sequenceNumber < 0 {
				resp.IsLast = true
			}
		}
	} else if messageType == 0xf { // error message (15)
		// 解析错误信息
		code := int32(binary.BigEndian.Uint32(payload[0:4]))
		errMsg := payload[8:] // 错误消息从第8个字节开始

		// 如果是压缩的，解压缩
		if messageCompression == 1 {
			errMsg = gzipDecompress(errMsg)
		}

		log.Errorf("服务端错误 (代码: %d): %s", code, string(errMsg))
		err = serverError{code: code, message: string(errMsg)}
		return
	} else if messageType == 0xc { // frontend server response (12)
		// 解析前端消息
		msgSize := int32(binary.BigEndian.Uint32(payload[0:4]))
		payload = payload[4:]

		// 如果是压缩的，解压缩
		if messageCompression == 1 {
			payload = gzipDecompress(payload)
		}

		// 记录前端消息
		if os.Getenv("DEBUG") == "1" {
			log.Debugf("前端消息大小: %d", msgSize)
			log.Debugf("前端消息内容: %s", string(payload))
		}
	} else {
		// 未知消息类型
		log.Warnf("未知消息类型: %d", messageType)
		err = fmt.Errorf("未知消息类型: %d", messageType)
		return
	}

	return
}

// saveAudioToTmp 将音频数据保存到tmp目录
func saveAudioToTmp(audioData []byte, format string) error {
	// 确保tmp目录存在
	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("创建tmp目录失败: %v", err)
	}

	// 生成唯一文件名
	timestamp := time.Now().Format("20060102_150405")
	uuid := generateUUID()
	filename := filepath.Join(tmpDir, fmt.Sprintf("audio_%s_%s.%s", timestamp, uuid[:8], format))

	// 写入文件
	if err := os.WriteFile(filename, audioData, 0644); err != nil {
		return fmt.Errorf("写入音频文件失败: %v", err)
	}

	log.Debugf("音频文件已保存: %s", filename)
	return nil
}

// SetVoice 设置音色参数
func (p *DoubaoWSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	if voice, ok := voiceConfig["voice"].(string); ok && voice != "" {
		p.Voice = voice
		return nil
	}
	return fmt.Errorf("无效的音色配置: 缺少 voice")
}

// Close 关闭资源，释放连接
func (p *DoubaoWSProvider) Close() error {
	p.clearConnection()
	return nil
}

// IsValid 检查资源是否有效
func (p *DoubaoWSProvider) IsValid() bool {
	p.connMutex.RLock()
	conn := p.conn
	p.connMutex.RUnlock()

	// 检查连接是否存在
	return conn != nil
}
