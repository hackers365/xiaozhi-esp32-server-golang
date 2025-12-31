package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/audio"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/util"

	"github.com/gorilla/websocket"
)

var detectStartTs int64
var waitInput = make(chan struct{}, 1)
var status = "idle"

// 消息类型常量
const (
	MessageTypeHello  = "hello"
	MessageTypeListen = "listen"
	MessageTypeAbort  = "abort"
	MessageTypeIot    = "iot"
	MessageTypeMcp    = "mcp"
)

// 消息状态常量
const (
	MessageStateStart   = "start"
	MessageStateStop    = "stop"
	MessageStateDetect  = "detect"
	MessageStateSuccess = "success"
	MessageStateError   = "error"
	MessageStateAbort   = "abort"
)

// ClientMessage 表示客户端消息
type ClientMessage struct {
	Type        string          `json:"type"`
	DeviceID    string          `json:"device_id"`
	Text        string          `json:"text,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	State       string          `json:"state,omitempty"`
	Token       string          `json:"token,omitempty"`
	DeviceMac   string          `json:"device_mac,omitempty"`
	Version     int             `json:"version,omitempty"`
	Transport   string          `json:"transport,omitempty"`
	AudioParams *AudioFormat    `json:"audio_params,omitempty"`
	Features    map[string]bool `json:"features,omitempty"`
	PayLoad     json.RawMessage `json:"payload,omitempty"`
}

// ServerMessage 表示服务器消息
type ServerMessage struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	State       string          `json:"state,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Transport   string          `json:"transport,omitempty"`
	AudioFormat *AudioFormat    `json:"audio_format,omitempty"`
	PayLoad     json.RawMessage `json:"payload,omitempty"`
}

// AudioFormat 表示音频格式
type AudioFormat struct {
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
	Format        string `json:"format"`
}

// Opus编码常量
var (
	// Opus编码的采样率
	SampleRate = 16000
	// 音频通道数
	Channels = 1
	// 每帧持续时间(毫秒)
	FrameDurationMs = 20
	// PCM缓冲区大小 = 采样率 * 通道数 * 帧持续时间(秒)
	PCMBufferSize = SampleRate * Channels * FrameDurationMs / 1000

	mode = "auto"

	addMcp = false
)

var speectText = "你好测试"
var clientId = "e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d"
var token = "test-token"

func main() {
	// 解析命令行参数
	serverAddr := flag.String("server", "ws://localhost:8989/xiaozhi/v1/", "服务器地址")
	deviceID := flag.String("device", "test-device-001", "设备ID")
	audioFile := flag.String("audio", "../test.wav", "音频文件路径")
	text := flag.String("text", "你好测试", "文本")
	modeFlag := flag.String("mode", "auto", "模式")
	sampleRate := flag.Int("sample_rate", 16000, "sampleRate")
	frameDurationsMs := flag.Int("frame_ms", 20, "frame duration ms")
	addMcpFlag := flag.Bool("mcp", false, "是否启用mcp")

	flag.Parse()

	fmt.Printf("运行小智客户端\n服务器: %s\n设备ID: %s\n音频文件: %s\n",
		*serverAddr, *deviceID, *audioFile)

	speectText = *text
	SampleRate = *sampleRate
	FrameDurationMs = *frameDurationsMs
	mode = *modeFlag
	addMcp = *addMcpFlag

	// 运行客户端
	if err := runClient(*serverAddr, *deviceID, *audioFile); err != nil {
		log.Fatalf("客户端运行失败: %v", err)
	}
}

var OpusData [][]byte
var firstRecvFrame bool

// runClient 运行小智客户端
func runClient(serverAddr, deviceID, audioFile string) error {
	OpusData = [][]byte{}
	// 构建WebSocket URL
	wsURL := serverAddr
	fmt.Printf("正在连接服务器: %s\n", wsURL)

	// 设置HTTP头
	header := http.Header{}
	header.Set("Device-Id", deviceID)
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+token)
	header.Set("Protocol-Version", "1")
	header.Set("Client-Id", clientId)

	// 连接WebSocket服务器
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("连接失败: %v", err)
	}
	defer conn.Close()

	fmt.Println("已连接到服务器")

	mcpSendMsgChan := make(chan []byte, 10)
	mcpRecvMsgChan := make(chan []byte, 10)

	go func() {
		for msg := range mcpSendMsgChan {
			fmt.Printf("发送mcp消息: %s\n", string(msg))
			/*respMsg := ClientMessage{
				Type:     MessageTypeMcp,
				DeviceID: deviceID,
				PayLoad:  msg,
			}
			jsonByte, _ := json.Marshal(respMsg)*/
			conn.WriteMessage(websocket.TextMessage, msg)
		}
	}()

	// 设置消息处理
	done := make(chan struct{})

	var startTs int64
	_ = startTs
	// 启动一个协程来处理从服务器接收的消息
	go func() {
		var iLock sync.Mutex
		defer close(done)
		//var recvInterval int64
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("读取消息失败: %v\n", err)
				return
			}

			if messageType == websocket.TextMessage {
				fmt.Printf("收到服务器消息: %+v\n", string(message))
				var serverMsg ServerMessage
				if err := json.Unmarshal(message, &serverMsg); err != nil {
					fmt.Printf("解析消息失败: %v\n", err)
					continue
				}

				if serverMsg.Type == "mcp" {
					select {
					case mcpRecvMsgChan <- serverMsg.PayLoad:
					default:
						fmt.Printf("mcp消息队列已满, 丢弃消息: %s\n", string(serverMsg.PayLoad))
					}
				}

				if serverMsg.Type == "tts" && serverMsg.State == "stop" {
					//OpusToWav(OpusData, 24000, 1, "ws_output_24000.wav")
					select {
					case waitInput <- struct{}{}:
					default:
					}
				}
			} else if messageType == websocket.BinaryMessage {
				if !firstRecvFrame {
					iLock.Lock()
					if !firstRecvFrame {
						firstRecvFrame = true
						fmt.Printf("首帧到达时间: %d 毫秒\n", time.Now().UnixMilli()-detectStartTs)
					}
					iLock.Unlock()
					//os.WriteFile("ws_output_first_frame.wav", message, 0644)
				}
				OpusData = append(OpusData, message)
				//fmt.Printf("收到音频数据: %d 字节, 间隔: %d 毫秒\n", len(message), time.Now().UnixMilli()-recvInterval)
				//recvInterval = time.Now().UnixMilli()
			}
		}
	}()

	go func() {
		NewMcpServer(mcpSendMsgChan, mcpRecvMsgChan)
	}()

	// 发送hello消息
	helloMsg := ClientMessage{
		Type:      MessageTypeHello,
		DeviceID:  deviceID,
		Transport: "websocket",
		Version:   1,
		Features:  map[string]bool{},
		AudioParams: &AudioFormat{
			SampleRate:    SampleRate,
			Channels:      Channels,
			FrameDuration: FrameDurationMs,
			Format:        "opus",
		},
	}

	if addMcp {
		helloMsg.Features["mcp"] = true
	}

	if err := sendJSONMessage(conn, helloMsg); err != nil {
		return fmt.Errorf("发送hello消息失败: %v", err)
	}

	// 等待接收服务器响应
	time.Sleep(1 * time.Second)

	fmt.Println("开始发送音频数据...")

	// 读取并发送音频文件（使用Opus编码）
	/*if err := sendWavFileWithOpusEncoding(conn, audioFile); err != nil {
		return fmt.Errorf("发送音频数据失败: %v\n", err)
	}*/

	waitInput <- struct{}{}
	if err := sendTextToSpeech(conn, deviceID); err != nil {
		return fmt.Errorf("发送文本到语音失败: %v", err)
	}
	/*
		for i := 0; i < 1; i++ {
			detectStartTs = time.Now().UnixMilli()
			if err := sendListenDetect(conn, deviceID, speectText); err != nil {
				return fmt.Errorf("发送文本失败: %v", err)
			}
			time.Sleep(5000 * time.Millisecond)
		}

		saveOpusData()
		OpusToWav(OpusData, 24000, 1, "ws_output_24000.wav")
	*/

	time.Sleep(100 * time.Millisecond)
	// 发送listen stop消息
	/*listenStopMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateStop,
	}

	if err := sendJSONMessage(conn, listenStopMsg); err != nil {
		return fmt.Errorf("发送listen stop消息失败: %v", err)
	}*/

	fmt.Println("已发送停止消息，等待服务器响应...")

	// 等待一段时间，接收服务器的响应
	time.Sleep(30 * time.Second)

	return nil
}

func saveOpusData() error {
	f, err := os.Create("opus_ws.data")
	if err != nil {
		return err
	}
	defer f.Close()

	for _, data := range OpusData {
		f.Write(data)
	}

	f.Close()

	return nil
}

func sendListenStart(conn *websocket.Conn, deviceID string, mode string) error {
	// 发送listen start消息
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateStart,
		Mode:     mode,
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}
	return nil
}

func sendListenStop(conn *websocket.Conn, deviceID string) error {
	// 发送listen start消息
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateStop,
		Mode:     "manual",
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen stop消息失败: %v", err)
	}

	return nil
}

func sendAbort(conn *websocket.Conn, deviceID string) error {
	// 发送listen start消息
	listenStartMsg := ClientMessage{
		Type:     MessageTypeAbort,
		DeviceID: deviceID,
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen start消息失败: %v", err)
	}
	return nil
}

func sendListenDetect(conn *websocket.Conn, deviceID string, text string) error {
	// 发送listen start消息
	listenStartMsg := ClientMessage{
		Type:     MessageTypeListen,
		DeviceID: deviceID,
		State:    MessageStateDetect,
		Text:     text,
	}

	if err := sendJSONMessage(conn, listenStartMsg); err != nil {
		return fmt.Errorf("发送listen detect消息失败: %v", err)
	}
	return nil
}

// 发送JSON消息
func sendJSONMessage(conn *websocket.Conn, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Printf("发送消息: %s\n", string(data))
	return conn.WriteMessage(websocket.TextMessage, data)
}

// 读取WAV文件并使用Opus编码发送
func sendWavFileWithOpusEncoding(conn *websocket.Conn, filePath string) error {
	// 打开WAV文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开WAV文件失败: %v", err)
	}
	defer file.Close()

	// 读取文件内容
	fileContent, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("读取文件内容失败: %v", err)
	}
	fmt.Printf("文件内容长度: %d\n", len(fileContent))
	file.Close()

	opusFrames, err := util.WavToOpus(fileContent, SampleRate, Channels, 0)
	if err != nil {
		return fmt.Errorf("转换WAV文件失败: %v", err)
	}

	fmt.Printf("转换后的Opus帧数: %d\n", len(opusFrames))

	for i, frame := range opusFrames {
		fmt.Printf("Opus帧 %d 长度: %d\n", i, len(frame))
		// 发送Opus帧
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return fmt.Errorf("发送Opus帧失败: %v", err)
		}
		// 控制发送速率，模拟实时音频流
		//time.Sleep(60 * time.Millisecond)
	}

	//持续发送空的音频数据
	emptyFrame := make([]byte, 50)
	for {
		if err := conn.WriteMessage(websocket.BinaryMessage, emptyFrame); err != nil {
			return fmt.Errorf("发送空音频数据失败: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	return nil
}

// 读取并发送音频文件（原始方式，不使用Opus编码）
func sendAudioFile(conn *websocket.Conn, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开音频文件失败: %v", err)
	}
	defer file.Close()

	// 直接读取文件内容并分块发送
	// 每次读取并发送一个固定大小的块
	const chunkSize = 4096
	buffer := make([]byte, chunkSize)

	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取音频数据失败: %v", err)
		}

		if n > 0 {
			// 发送二进制音频数据
			if err := conn.WriteMessage(websocket.BinaryMessage, buffer[:n]); err != nil {
				return fmt.Errorf("发送音频数据失败: %v", err)
			}

			// 控制发送速率，模拟实时音频流
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

func genEmptyOpusData(sampleRate int, channels int, frameDurationMs int, count int) []byte {
	audioProcesser, err := audio.GetAudioProcesser(sampleRate, channels, frameDurationMs)
	if err != nil {
		return nil
	}

	frameSize := sampleRate * channels * frameDurationMs / 1000

	pcmFrame := make([]int16, frameSize)
	opusFrame := make([]byte, 1000)

	n, err := audioProcesser.Encoder(pcmFrame, opusFrame)
	if err != nil {
		return nil
	}

	tmp := make([]byte, n)
	copy(tmp, opusFrame)
	return tmp
}

// 调用tts服务生成语音, 并编码至opus发送至服务端
func sendTextToSpeech(conn *websocket.Conn, deviceID string) error {
	cosyVoiceConfig := map[string]interface{}{
		"api_url":        "https://tts.linkerai.cn/tts",
		"spk_id":         "OUeAo1mhq6IBExi",
		"frame_duration": FrameDurationMs,
		"target_sr":      SampleRate,
		"audio_format":   "mp3",
		"instruct_text":  "你好",
	}
	_ = cosyVoiceConfig
	/**
		    "edge": {
	      "voice": "zh-CN-XiaoxiaoNeural",
	      "rate": "+0%",
	      "volume": "+0%",
	      "pitch": "+0Hz",
	      "connect_timeout": 10,
	      "receive_timeout": 60
	    }
	*/
	edgeConfig := map[string]interface{}{
		"voice":           "zh-CN-XiaoxiaoNeural",
		"rate":            "+0%",
		"volume":          "+0%",
		"pitch":           "+0Hz",
		"connect_timeout": 10,
		"receive_timeout": 60,
	}
	_ = edgeConfig
	//调用tts服务生成语音
	ttsProvider, err := tts.GetTTSProvider("edge", edgeConfig)
	if err != nil {
		return fmt.Errorf("获取tts服务失败: %v", err)
	}

	/*
		audioData, err := ttsProvider.TextToSpeech(context.Background(), "你叫什么名字?")
		if err != nil {
			fmt.Printf("生成语音失败: %v\n", err)
			return fmt.Errorf("生成语音失败: %v", err)
		}
	*/

	emptyOpusData := genEmptyOpusData(SampleRate, 1, FrameDurationMs, 1000)

	genAndSendAudio := func(msg string, count int) error {
		audioChan, err := ttsProvider.TextToSpeechStream(context.Background(), msg, SampleRate, 1, FrameDurationMs)
		if err != nil {
			fmt.Printf("生成语音失败: %v\n", err)
			return fmt.Errorf("生成语音失败: %v", err)
		}

		for audioData := range audioChan {
			//fmt.Printf("发送语音数据长度: %d\n", len(audioData))
			conn.WriteMessage(websocket.BinaryMessage, audioData)
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		detectStartTs = time.Now().UnixMilli()

		for i := 0; i <= count; i++ {
			conn.WriteMessage(websocket.BinaryMessage, emptyOpusData)
			time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
		}

		firstRecvFrame = false
		return nil
	}

	//发送detect 消息
	sendListenDetect(conn, deviceID, "你好小智")

	// 新增：等待用户输入文本
	reader := bufio.NewReader(os.Stdin)

	var stopEmptyOpusChan = make(chan struct{})
	var resumeChan = make(chan struct{})
	go func() {
		if mode == "realtime" {
			//持续发送emptyOpusData, 直到收到 停止信号
			for {
				select {
				case <-stopEmptyOpusChan:
					resumeChan <- struct{}{}
					<-resumeChan
				default:
					conn.WriteMessage(websocket.BinaryMessage, emptyOpusData)
					time.Sleep(time.Duration(FrameDurationMs) * time.Millisecond)
				}
			}
		}
	}()

	for {

		fmt.Print("请输入要合成的文本（回车发送，直接回车退出）：")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			continue
		}
		input = strings.TrimSpace(input)
		if input == "" {
			//发送abort
			sendAbort(conn, deviceID)
			select {
			case waitInput <- struct{}{}:
				status = "idle"
			default:
			}
			continue
		}
		f := func() {
			if mode == "realtime" {
				stopEmptyOpusChan <- struct{}{}
				<-resumeChan
			}
			sendListenStart(conn, deviceID, mode)
			genAndSendAudio(input, 100)
			if mode == "manual" {
				sendListenStop(conn, deviceID)
			}
			if mode == "realtime" {
				resumeChan <- struct{}{}
			}
		}
		if mode == "realtime" {
			go f()
			continue
		}
		select {
		case <-waitInput:
			go f()
		}
	}

	return nil
}
