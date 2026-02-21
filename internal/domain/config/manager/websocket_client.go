package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	cmap "github.com/orcaman/concurrent-map/v2"

	"xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
	"xiaozhi-esp32-server-golang/pkg/buildinfo"
)

type MessageHandleFunc func(*WebSocketRequest) (string, error)

type WebSocketClient struct {
	conn           *websocket.Conn
	baseURL        string
	requestTimeout time.Duration
	responseChans  map[string]chan *WebSocketResponse
	callbacks      map[string]func(*WebSocketResponse)
	requestHandler func(*WebSocketRequest) // 处理收到的请求
	mu             sync.RWMutex
	writeMu        sync.Mutex // 保护WebSocket写入操作，防止并发写入
	isConnected    bool
	connectMu      sync.Mutex
	messageQueue   chan *WebSocketRequest
	workers        sync.WaitGroup

	messageHandle cmap.ConcurrentMap[string, MessageHandleFunc]
	uuid          string

	// 重连相关字段
	retryStopChan  chan struct{}  // 重连协程停止信号
	retryWg        sync.WaitGroup // 重连协程等待组
	retryMu        sync.Mutex     // 保护重连相关操作
	isRetrying     bool           // 是否正在重连
	isShuttingDown bool           // 是否正在关闭（主动断开，不重连）
}

type WebSocketRequest struct {
	ID      string                 `json:"id"`
	Method  string                 `json:"method"`
	Path    string                 `json:"path"`
	Headers map[string]string      `json:"headers,omitempty"`
	Body    map[string]interface{} `json:"body,omitempty"`
}

type WebSocketResponse struct {
	ID      string                 `json:"id"`
	Status  int                    `json:"status"`
	Headers map[string]string      `json:"headers,omitempty"`
	Body    map[string]interface{} `json:"body,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

var (
	defaultClient           *WebSocketClient
	clientOnce              sync.Once
	systemConfigPushHandler func(map[string]interface{})
)

// SetSystemConfigPushHandler 设置收到 system_config 推送时的回调（主程序用于合并到 viper 等），由 user_config 在 Init 时注入
func SetSystemConfigPushHandler(fn func(map[string]interface{})) {
	systemConfigPushHandler = fn
}

func GetDefaultClient() *WebSocketClient {
	clientOnce.Do(func() {
		defaultClient = NewWebSocketClient()
	})
	return defaultClient
}

func NewWebSocketClient() *WebSocketClient {
	// 优先从环境变量获取，如果环境变量不存在则从配置获取
	baseURL := util.GetBackendURL()
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	return &WebSocketClient{
		baseURL:        baseURL,
		requestTimeout: 30 * time.Second,
		responseChans:  make(map[string]chan *WebSocketResponse),
		callbacks:      make(map[string]func(*WebSocketResponse)),
		messageQueue:   make(chan *WebSocketRequest, 100),
		messageHandle:  cmap.New[MessageHandleFunc](),
		uuid:           uuid.New().String(),
		retryStopChan:  make(chan struct{}),
		isRetrying:     false,
	}
}

func NewWebSocketClientWithHandler(requestHandler func(*WebSocketRequest)) *WebSocketClient {
	client := NewWebSocketClient()
	client.requestHandler = requestHandler
	return client
}

func (c *WebSocketClient) Connect(ctx context.Context) error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	if c.isConnected {
		return nil
	}

	// 将HTTP URL转换为WebSocket URL
	wsURL := "ws://" + c.baseURL[7:] + "/ws" // 去掉 "http://" 并添加 "/ws"

	// 建立WebSocket连接
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Origin": []string{c.baseURL},
		"UUID":   []string{c.uuid},
	})
	if err != nil {
		return fmt.Errorf("WebSocket连接失败: %v", err)
	}

	c.conn = conn
	c.isConnected = true

	// 设置ping处理器
	conn.SetPongHandler(func(appData string) error {
		log.Debugf("收到pong消息")
		return nil
	})

	// 启动消息处理循环
	go c.handleMessages()

	// 启动消息发送工作线程
	c.startWorkers()

	// 启动心跳检测
	go c.startHeartbeat()

	log.Debugf("WebSocket客户端已连接到: %s", wsURL)
	return nil
}

func (c *WebSocketClient) Disconnect() error {
	return c.disconnect(false)
}

// disconnect 内部断开连接方法
// manualDisconnect: true表示主动断开（不触发重连），false表示错误断开（触发重连）
func (c *WebSocketClient) disconnect(manualDisconnect bool) error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	if !c.isConnected {
		return nil
	}

	if manualDisconnect {
		c.isShuttingDown = true
	}

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			log.Debugf("关闭WebSocket连接时出错: %v", err)
		}
		c.conn = nil
	}

	c.isConnected = false
	c.mu.Lock()
	// 关闭所有响应通道
	for _, ch := range c.responseChans {
		close(ch)
	}
	c.responseChans = make(map[string]chan *WebSocketResponse)
	c.callbacks = make(map[string]func(*WebSocketResponse))
	c.mu.Unlock()

	// 停止工作线程
	close(c.messageQueue)
	c.workers.Wait()
	// 重新创建消息队列
	c.messageQueue = make(chan *WebSocketRequest, 100)

	log.Debugf("WebSocket连接已断开")
	return nil
}

func (c *WebSocketClient) IsConnected() bool {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()
	return c.isConnected
}

func (c *WebSocketClient) SendRequest(ctx context.Context, method, path string, body map[string]interface{}) (*WebSocketResponse, error) {
	if !c.IsConnected() {
		if err := c.Connect(ctx); err != nil {
			return nil, fmt.Errorf("连接失败: %v", err)
		}
	}

	// 生成UUID作为请求ID
	requestID := uuid.New().String()

	request := WebSocketRequest{
		ID:     requestID,
		Method: method,
		Path:   path,
		Body:   body,
	}

	// 创建响应通道
	responseChan := make(chan *WebSocketResponse, 1)
	c.mu.Lock()
	c.responseChans[requestID] = responseChan
	c.mu.Unlock()

	// 清理响应通道
	defer func() {
		c.mu.Lock()
		delete(c.responseChans, requestID)
		c.mu.Unlock()
		close(responseChan)
	}()

	// 发送请求（使用写入锁保护）
	c.writeMu.Lock()
	err := c.conn.WriteJSON(request)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}

	// 等待响应
	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(c.requestTimeout):
		return nil, fmt.Errorf("请求超时")
	case <-ctx.Done():
		return nil, fmt.Errorf("上下文取消")
	}
}

// 便捷方法 - 使用WebSocket原生ping
func (c *WebSocketClient) Ping() error {
	if !c.IsConnected() {
		return fmt.Errorf("WebSocket未连接")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
}

func (c *WebSocketClient) GetStatus(ctx context.Context) (*WebSocketResponse, error) {
	return c.SendRequest(ctx, "GET", "/api/ws/status", nil)
}

func (c *WebSocketClient) Echo(ctx context.Context, message string) (*WebSocketResponse, error) {
	return c.SendRequest(ctx, "POST", "/api/ws/echo", map[string]interface{}{
		"message": message,
	})
}

// 全局便捷方法
func ConnectManagerWebSocket(ctx context.Context) error {
	return GetDefaultClient().Connect(ctx)
}

func DisconnectManagerWebSocket() error {
	client := GetDefaultClient()
	client.StopReconnect()
	return client.disconnect(true) // 主动断开，不触发重连
}

func SendManagerRequest(ctx context.Context, method, path string, body map[string]interface{}) (*WebSocketResponse, error) {
	return GetDefaultClient().SendRequest(ctx, method, path, body)
}

func ManagerWebSocketPing(ctx context.Context) error {
	return GetDefaultClient().Ping()
}

func ManagerWebSocketStatus(ctx context.Context) (*WebSocketResponse, error) {
	return GetDefaultClient().GetStatus(ctx)
}

func ManagerWebSocketEcho(ctx context.Context, message string) (*WebSocketResponse, error) {
	return GetDefaultClient().Echo(ctx, message)
}

func IsManagerWebSocketConnected() bool {
	return GetDefaultClient().IsConnected()
}

func SendDeviceRequest(ctx context.Context, path string, body map[string]interface{}) (*WebSocketResponse, error) {
	return GetDefaultClient().SendRequest(ctx, "POST", path, body)
}

// startWorkers 启动消息发送工作线程
func (c *WebSocketClient) startWorkers() {
	workerCount := 3 // 启动3个工作线程

	for i := 0; i < workerCount; i++ {
		c.workers.Add(1)
		go func(workerID int) {
			defer c.workers.Done()

			log.Debugf("Manager WebSocket工作线程 %d 已启动", workerID)

			for request := range c.messageQueue {
				if !c.IsConnected() {
					log.Debugf("工作线程 %d: WebSocket未连接，丢弃请求", workerID)
					continue
				}

				// 发送请求（使用写入锁保护）
				c.writeMu.Lock()
				err := c.conn.WriteJSON(request)
				c.writeMu.Unlock()
				if err != nil {
					log.Debugf("工作线程 %d: 发送请求失败: %v", workerID, err)
					// 连接可能已断开，触发重连
					c.handleConnectionError()
					continue
				}

				log.Debugf("工作线程 %d: 已发送请求 %s", workerID, request.ID)
			}

			log.Debugf("Manager WebSocket工作线程 %d 已停止", workerID)
		}(i)
	}
}

// handleConnectionError 处理连接错误
func (c *WebSocketClient) handleConnectionError() {
	if c.IsConnected() {
		log.Warn("检测到WebSocket连接错误，正在断开连接...")
		c.disconnect(false) // 错误断开，会触发重连
		// 触发重连
		c.triggerReconnect()
	}
}

// startHeartbeat 启动心跳检测
func (c *WebSocketClient) startHeartbeat() {
	ticker := time.NewTicker(30 * time.Second) // 每30秒发送一次ping
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !c.IsConnected() {
				return
			}

			// 发送ping消息
			c.writeMu.Lock()
			err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
			c.writeMu.Unlock()

			if err != nil {
				log.Warnf("发送ping失败，连接可能已断开: %v", err)
				c.disconnect(false) // 错误断开，会触发重连
				// 触发重连
				c.triggerReconnect()
				return
			}
			log.Debugf("发送ping消息成功")

		case <-c.retryStopChan:
			return
		}
	}
}

// triggerReconnect 触发重连（非阻塞）
func (c *WebSocketClient) triggerReconnect() {
	c.retryMu.Lock()
	defer c.retryMu.Unlock()

	// 如果正在关闭，不触发重连
	if c.isShuttingDown {
		log.Debug("正在关闭中，不触发重连")
		return
	}

	// 如果已经在重连，不重复触发
	if c.isRetrying {
		return
	}

	c.isRetrying = true
	// 启动重连协程
	c.retryWg.Add(1)
	go c.startReconnectLoop()
}

// startReconnectLoop 启动重连循环（使用指数退避算法）
func (c *WebSocketClient) startReconnectLoop() {
	defer func() {
		c.retryMu.Lock()
		c.isRetrying = false
		c.retryMu.Unlock()
		c.retryWg.Done()
	}()

	// 硬编码的退避算法参数
	initialDelay := 3 * time.Second // 初始延迟3秒
	maxDelay := 1 * time.Minute     // 最大延迟1分钟
	backoffMultiplier := 2.0        // 退避倍数

	delay := initialDelay
	retryCount := 0

	log.Infof("Manager WebSocket连接重试协程已启动")

	for {
		// 检查是否应该停止重连
		select {
		case <-c.retryStopChan:
			log.Info("收到停止信号，停止重连")
			return
		default:
		}

		// 如果正在关闭，停止重连
		c.retryMu.Lock()
		shuttingDown := c.isShuttingDown
		c.retryMu.Unlock()
		if shuttingDown {
			log.Info("正在关闭中，停止重连")
			return
		}

		// 如果已经连接，停止重连
		if c.IsConnected() {
			log.Info("Manager WebSocket连接已恢复，停止重连")
			return
		}

		retryCount++
		log.Warnf("Manager WebSocket连接失败 (第%d次)，等待 %v 后重试连接...", retryCount, delay)

		// 等待延迟时间
		select {
		case <-time.After(delay):
			// 继续重连
		case <-c.retryStopChan:
			log.Info("收到停止信号，停止重连")
			return
		}

		// 尝试连接
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := c.Connect(ctx)
		cancel()

		if err != nil {
			log.Warnf("Manager WebSocket连接失败 (第%d次): %v", retryCount, err)
			// 计算下一次延迟时间（指数退避）
			delay = time.Duration(float64(delay) * backoffMultiplier)
			if delay > maxDelay {
				delay = maxDelay
			}
			continue
		}

		// 连接成功
		log.Info("Manager WebSocket连接成功")
		return
	}
}

// StopReconnect 停止重连协程
func (c *WebSocketClient) StopReconnect() {
	c.retryMu.Lock()
	c.isShuttingDown = true
	shouldClose := c.retryStopChan != nil
	c.retryMu.Unlock()

	if shouldClose {
		// 使用 select 避免重复关闭通道
		select {
		case <-c.retryStopChan:
			// 通道已经关闭
		default:
			close(c.retryStopChan)
		}
		c.retryWg.Wait()
		log.Info("Manager WebSocket重连协程已优雅关闭")
	}
}

// SendRequestWithCallback 发送请求并使用回调处理响应
func (c *WebSocketClient) SendRequestWithCallback(ctx context.Context, method, path string, body map[string]interface{}, callback func(*WebSocketResponse)) error {
	if !c.IsConnected() {
		if err := c.Connect(ctx); err != nil {
			return fmt.Errorf("连接失败: %v", err)
		}
	}

	// 生成UUID作为请求ID
	requestID := uuid.New().String()

	request := WebSocketRequest{
		ID:     requestID,
		Method: method,
		Path:   path,
		Body:   body,
	}

	// 注册回调
	c.mu.Lock()
	c.callbacks[requestID] = callback
	c.mu.Unlock()

	// 清理回调
	defer func() {
		c.mu.Lock()
		delete(c.callbacks, requestID)
		c.mu.Unlock()
	}()

	// 将请求放入队列
	select {
	case c.messageQueue <- &request:
		log.Debugf("请求 %s 已加入队列", requestID)
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("消息队列已满，请求超时")
	case <-ctx.Done():
		return fmt.Errorf("上下文取消")
	}
}

// SendRequestAsync 异步发送请求
func (c *WebSocketClient) SendRequestAsync(ctx context.Context, method, path string, body map[string]interface{}) (string, error) {
	if !c.IsConnected() {
		if err := c.Connect(ctx); err != nil {
			return "", fmt.Errorf("连接失败: %v", err)
		}
	}

	// 生成UUID作为请求ID
	requestID := uuid.New().String()

	request := WebSocketRequest{
		ID:     requestID,
		Method: method,
		Path:   path,
		Body:   body,
	}

	// 将请求放入队列
	select {
	case c.messageQueue <- &request:
		log.Debugf("异步请求 %s 已加入队列", requestID)
		return requestID, nil
	case <-time.After(5 * time.Second):
		return "", fmt.Errorf("消息队列已满，请求超时")
	case <-ctx.Done():
		return "", fmt.Errorf("上下文取消")
	}
}

// GetResponse 获取指定请求ID的响应（用于异步请求）
func (c *WebSocketClient) GetResponse(requestID string, timeout time.Duration) (*WebSocketResponse, error) {
	responseChan := make(chan *WebSocketResponse, 1)

	// 注册临时回调
	c.mu.Lock()
	c.callbacks[requestID] = func(response *WebSocketResponse) {
		responseChan <- response
	}
	c.mu.Unlock()

	// 清理回调
	defer func() {
		c.mu.Lock()
		delete(c.callbacks, requestID)
		c.mu.Unlock()
		close(responseChan)
	}()

	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("等待响应超时")
	}
}

// handleSystemConfigPush 处理服务端推送的系统配置变更，异步调用已注册的回调
func (c *WebSocketClient) handleSystemConfigPush(data map[string]interface{}) {
	if systemConfigPushHandler == nil {
		log.Debugf("收到 system_config 推送，但未注册处理回调")
		return
	}
	go systemConfigPushHandler(data)
}

// handleMessages 处理接收到的WebSocket消息
func (c *WebSocketClient) handleMessages() {
	for {
		if !c.isConnected {
			return
		}

		// 读取消息类型
		messageType, reader, err := c.conn.NextReader()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Debugf("WebSocket读取错误: %v", err)
			}
			c.disconnect(false) // 错误断开，会触发重连
			// 触发重连
			c.triggerReconnect()
			return
		}

		// 处理不同类型的消息
		switch messageType {
		case websocket.TextMessage:
			// 处理JSON消息
			var rawMessage map[string]interface{}
			if err := json.NewDecoder(reader).Decode(&rawMessage); err != nil {
				log.Errorf("解析JSON消息失败: %v", err)
				continue
			}

			// 根据消息类型判断：服务端推送(system_config)、请求、响应
			if msgType, _ := rawMessage["type"].(string); msgType == "system_config" {
				if data, ok := rawMessage["data"].(map[string]interface{}); ok {
					c.handleSystemConfigPush(data)
				} else {
					log.Warnf("收到 system_config 推送但 data 格式无效")
				}
			} else if method, exists := rawMessage["method"]; exists && method != nil {
				// 这是收到的请求
				c.handleIncomingRequest(rawMessage)
			} else if status, exists := rawMessage["status"]; exists && status != nil {
				// 这是收到的响应
				c.handleIncomingResponse(rawMessage)
			} else {
				log.Warnf("收到无法识别的WebSocket消息: %+v", rawMessage)
			}

		case websocket.PingMessage:
			// 处理ping消息，自动回复pong（使用写入锁保护）
			log.Debugf("收到ping消息，自动回复pong")
			c.writeMu.Lock()
			err := c.conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(10*time.Second))
			c.writeMu.Unlock()
			if err != nil {
				log.Errorf("发送pong失败: %v", err)
			}

		case websocket.PongMessage:
			// 处理pong消息
			log.Debugf("收到pong消息")

		case websocket.CloseMessage:
			// 处理关闭消息
			log.Debugf("收到关闭消息")
			c.disconnect(false) // 错误断开，会触发重连
			// 触发重连
			c.triggerReconnect()
			return

		default:
			log.Warnf("收到未知类型的WebSocket消息: %d", messageType)
		}
	}
}

// handleIncomingRequest 处理收到的请求
func (c *WebSocketClient) handleIncomingRequest(rawMessage map[string]interface{}) {
	var request WebSocketRequest
	if err := mapToStruct(rawMessage, &request); err != nil {
		log.Errorf("解析WebSocket请求失败: %v", err)
		return
	}

	log.Debugf("收到请求: ID=%s, Method=%s, Path=%s", request.ID, request.Method, request.Path)

	// 如果有注册的请求处理器，调用它
	if c.requestHandler != nil {
		go c.requestHandler(&request)
	} else {
		// 如果没有注册处理器，使用默认处理器处理已知路径
		c.handleDefaultRequest(&request)
	}
}

func (c *WebSocketClient) RegisterMessageHandler(ctx context.Context, path string, handler types.EventHandler) {
	f := func(request *WebSocketRequest) (string, error) {
		return handler(ctx, request.Path, request.Body)
	}
	c.messageHandle.Set(path, f)
}

// handleDefaultRequest 默认请求处理器
func (c *WebSocketClient) handleDefaultRequest(request *WebSocketRequest) {
	switch request.Path {
	case "/api/config/test":
		// 配置测试可能较耗时（VAD/ASR/LLM/TTS 串行执行），放入独立 goroutine 避免阻塞读循环，支持多请求并发
		go c.handleConfigTestRequest(request)

	case "/api/mcp/tools":
		// 处理MCP工具列表请求
		c.handleMcpToolListRequest(request)

	case "/api/mcp/call":
		// 处理MCP工具调用请求
		c.handleMcpToolCallRequest(request)

	case "/api/server/info":
		// 返回服务器信息
		uptimeSeconds := buildinfo.UptimeSeconds()
		response := map[string]interface{}{
			"server_name":    "xiaozhi-server",
			"version":        buildinfo.Version,
			"commit":         buildinfo.Commit,
			"build_time":     buildinfo.BuildTime,
			"started_at":     buildinfo.StartedAtRFC3339(),
			"uptime":         buildinfo.UptimeText(uptimeSeconds),
			"uptime_seconds": uptimeSeconds,
			"request_id":     request.ID,
		}

		if err := c.SendResponse(request.ID, 200, response, ""); err != nil {
			log.Errorf("发送服务器信息响应失败: %v", err)
		}

	case "/api/server/ping":
		// 简单的ping响应
		response := map[string]interface{}{
			"message": "pong from server",
			"time":    time.Now().Format(time.RFC3339),
		}

		if err := c.SendResponse(request.ID, 200, response, ""); err != nil {
			log.Errorf("发送ping响应失败: %v", err)
		}
	default:
		handler, exists := c.messageHandle.Get(request.Path)
		if exists {
			// 调用处理器并处理返回值
			result, err := handler(request)
			if err != nil {
				log.Errorf("处理请求 %s 失败: %v", request.Path, err)
				// 发送错误响应
				if err := c.SendResponse(request.ID, 500, nil, err.Error()); err != nil {
					log.Errorf("发送错误响应失败: %v", err)
				}
			} else {
				// 发送成功响应
				response := map[string]interface{}{
					"result": result,
				}
				if err := c.SendResponse(request.ID, 200, response, ""); err != nil {
					log.Errorf("发送成功响应失败: %v", err)
				}
			}
		} else {
			log.Warnf("收到未知的WebSocket请求路径: %s, ID: %s", request.Path, request.ID)

			// 发送404响应
			if err := c.SendResponse(request.ID, 404, nil, "Unknown endpoint"); err != nil {
				log.Errorf("发送错误响应失败: %v", err)
			}
		}
	}
}

// configTestTotalTimeout 配置测试整体超时（VAD+ASR+LLM+TTS 合计）
const configTestTotalTimeout = 90 * time.Second

// handleConfigTestRequest 处理配置测试请求：VAD/ASR/LLM/TTS 使用下发的配置与固定 WAV/文本执行轻量测试
func (c *WebSocketClient) handleConfigTestRequest(request *WebSocketRequest) {
	data, _ := request.Body["data"].(map[string]interface{})
	if data == nil {
		log.Debugf("[config_test] 请求 ID=%s 缺少 data 字段", request.ID)
		_ = c.SendResponse(request.ID, 400, nil, "缺少 data 字段")
		return
	}
	testText, _ := request.Body["test_text"].(string)
	// debug: 请求中各类型配置数量（不含 provider）
	log.Debugf("[config_test] 请求 ID=%s test_text=%q data 各类型条目数: vad=%d asr=%d llm=%d tts=%d",
		request.ID, testText,
		countConfigKeys(data["vad"]), countConfigKeys(data["asr"]),
		countConfigKeys(data["llm"]), countConfigKeys(data["tts"]))

	type configTestResult struct {
		vad, asr, llm, tts map[string]interface{}
	}
	done := make(chan configTestResult, 1)
	go func() {
		vadR, asrR, llmR, ttsR := RunConfigTest(data, testText)
		done <- configTestResult{vadR, asrR, llmR, ttsR}
	}()

	var vadR, asrR, llmR, ttsR map[string]interface{}
	select {
	case res := <-done:
		vadR, asrR, llmR, ttsR = res.vad, res.asr, res.llm, res.tts
	case <-time.After(configTestTotalTimeout):
		log.Warnf("[config_test] 请求 ID=%s 整体超时 %v", request.ID, configTestTotalTimeout)
		body := map[string]interface{}{
			"vad": map[string]interface{}{"_error": map[string]interface{}{"ok": false, "message": "配置测试总超时"}},
			"asr": map[string]interface{}{"_error": map[string]interface{}{"ok": false, "message": "配置测试总超时"}},
			"llm": map[string]interface{}{"_error": map[string]interface{}{"ok": false, "message": "配置测试总超时"}},
			"tts": map[string]interface{}{"_error": map[string]interface{}{"ok": false, "message": "配置测试总超时"}},
		}
		_ = c.SendResponse(request.ID, 200, body, "")
		return
	}

	// 请求中带了某类型但无任何可测配置时，返回 _none 便于前端展示原因
	fillEmptyConfigTestResult(data, "vad", vadR)
	fillEmptyConfigTestResult(data, "asr", asrR)
	fillEmptyConfigTestResult(data, "llm", llmR)
	fillEmptyConfigTestResult(data, "tts", ttsR)
	body := map[string]interface{}{
		"vad": vadR,
		"asr": asrR,
		"llm": llmR,
		"tts": ttsR,
	}
	log.Debugf("[config_test] 响应 ID=%s 各类型结果数: vad=%d asr=%d llm=%d tts=%d",
		request.ID, len(vadR), len(asrR), len(llmR), len(ttsR))
	_ = c.SendResponse(request.ID, 200, body, "")
}

// fillEmptyConfigTestResult 当请求包含该类型但测试结果为空时，写入 _none 条目
func fillEmptyConfigTestResult(data map[string]interface{}, typ string, result map[string]interface{}) {
	if _, has := data[typ]; !has || len(result) > 0 {
		return
	}
	msg := "未配置或未启用" + strings.ToUpper(typ)
	result["_none"] = map[string]interface{}{"ok": false, "message": msg}
	log.Debugf("[config_test] 类型 %s 无结果，已写入 _none: %s", typ, msg)
}

// countConfigKeys 统计 data 中除 provider 外的 config 条目数，用于 debug
func countConfigKeys(v interface{}) int {
	m, ok := v.(map[string]interface{})
	if !ok {
		return 0
	}
	n := 0
	for k := range m {
		if k != "provider" {
			n++
		}
	}
	return n
}

// handleIncomingResponse 处理收到的响应
func (c *WebSocketClient) handleIncomingResponse(rawMessage map[string]interface{}) {
	var response WebSocketResponse
	if err := mapToStruct(rawMessage, &response); err != nil {
		log.Errorf("解析WebSocket响应失败: %v", err)
		return
	}

	log.Debugf("收到响应: ID=%s, Status=%d", response.ID, response.Status)

	// 查找对应的响应通道和回调
	c.mu.RLock()
	responseChan, exists := c.responseChans[response.ID]
	callback, callbackExists := c.callbacks[response.ID]
	c.mu.RUnlock()

	if exists {
		select {
		case responseChan <- &response:
		default:
			log.Debugf("响应通道已满，丢弃响应: %s", response.ID)
		}
	}

	if callbackExists {
		go callback(&response)
	}

	if !exists && !callbackExists {
		log.Debugf("收到未知的响应ID: %s", response.ID)
	}
}

// SendResponse 发送响应给收到的请求
func (c *WebSocketClient) SendResponse(requestID string, status int, body map[string]interface{}, errorMsg string) error {
	if !c.IsConnected() {
		return fmt.Errorf("WebSocket未连接")
	}

	response := WebSocketResponse{
		ID:     requestID,
		Status: status,
		Body:   body,
		Error:  errorMsg,
	}

	// 使用写入锁保护
	c.writeMu.Lock()
	err := c.conn.WriteJSON(response)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}

	log.Debugf("已发送响应: ID=%s, Status=%d", requestID, status)
	return nil
}

// SetRequestHandler 设置请求处理器
func (c *WebSocketClient) SetRequestHandler(handler func(*WebSocketRequest)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestHandler = handler
}

// mapToStruct 辅助函数：将map转换为struct
func mapToStruct(data map[string]interface{}, target interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, target)
}

func toolInfoToSchemaMap(paramsOneOf interface{}) map[string]interface{} {
	if paramsOneOf == nil {
		return nil
	}

	raw, err := json.Marshal(paramsOneOf)
	if err != nil {
		return nil
	}

	decoded := map[string]interface{}{}
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}

	if openAPIV3, ok := decoded["openAPIV3"].(map[string]interface{}); ok {
		return openAPIV3
	}
	if openAPIV3, ok := decoded["open_api_v3"].(map[string]interface{}); ok {
		return openAPIV3
	}
	return decoded
}

func convertReportedToolsToToolList(reportedTools map[string]tool.InvokableTool) ([]map[string]interface{}, error) {
	toolList := make([]map[string]interface{}, 0)

	names := make([]string, 0, len(reportedTools))
	for name := range reportedTools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		invokable := reportedTools[name]
		toolInfo := map[string]interface{}{
			"name":        name,
			"description": fmt.Sprintf("MCP工具: %s", name),
			"schema":      true,
		}

		if info, err := invokable.Info(context.Background()); err == nil && info != nil {
			if info.Desc != "" {
				toolInfo["description"] = info.Desc
			}
			inputSchema := toolInfoToSchemaMap(info.ParamsOneOf)
			if inputSchema != nil {
				toolInfo["input_schema"] = inputSchema
			}
		}

		toolList = append(toolList, toolInfo)
	}

	return toolList, nil
}

func getDeviceMcpTools(deviceID string) ([]map[string]interface{}, error) {
	reportedTools, err := mcp.GetReportedToolsByDeviceID(deviceID)
	if err != nil {
		log.Errorf("获取设备上报MCP工具列表失败: %v", err)
		return nil, err
	}

	return convertReportedToolsToToolList(reportedTools)
}

func getAgentMcpTools(agentID string) ([]map[string]interface{}, error) {
	reportedTools, err := mcp.GetReportedToolsByAgentID(agentID)
	if err != nil {
		log.Errorf("获取智能体上报MCP工具列表失败: %v", err)
		return nil, err
	}

	return convertReportedToolsToToolList(reportedTools)
}

// handleMcpToolListRequest 处理MCP工具列表请求
func (c *WebSocketClient) handleMcpToolListRequest(request *WebSocketRequest) {
	// 从请求体中获取agent_id/device_id
	agentID := ""
	deviceID := ""
	if request.Body != nil {
		if id, ok := request.Body["agent_id"].(string); ok {
			agentID = id
		}
		if id, ok := request.Body["device_id"].(string); ok {
			deviceID = id
		}
	}

	if agentID == "" && deviceID == "" {
		log.Warnf("收到MCP工具列表请求，但缺少agent_id/device_id")
		if err := c.SendResponse(request.ID, 400, nil, "缺少agent_id或device_id参数"); err != nil {
			log.Errorf("发送错误响应失败: %v", err)
		}
		return
	}

	log.Infof("处理MCP工具列表请求，agent_id: %s, device_id: %s", agentID, deviceID)

	if agentID != "" && deviceID != "" {
		if err := c.SendResponse(request.ID, 400, nil, "agent_id与device_id不能同时传入"); err != nil {
			log.Errorf("发送错误响应失败: %v", err)
		}
		return
	}

	var (
		toolList []map[string]interface{}
		err      error
	)
	if deviceID != "" {
		toolList, err = getDeviceMcpTools(deviceID)
	} else {
		toolList, err = getAgentMcpTools(agentID)
	}
	if err != nil {
		log.Errorf("获取MCP工具列表失败: %v", err)
		if err := c.SendResponse(request.ID, 500, nil, fmt.Sprintf("获取工具列表失败: %v", err)); err != nil {
			log.Errorf("发送错误响应失败: %v", err)
		}
		return
	}

	// 构造响应
	response := map[string]interface{}{
		"agent_id":  agentID,
		"device_id": deviceID,
		"tools":     toolList,
		"count":     len(toolList),
	}

	// 发送响应
	if err := c.SendResponse(request.ID, 200, response, ""); err != nil {
		log.Errorf("发送MCP工具列表响应失败: %v", err)
	}
}

// 全局便捷方法（异步版本）
func SendManagerRequestAsync(ctx context.Context, method, path string, body map[string]interface{}) (string, error) {
	return GetDefaultClient().SendRequestAsync(ctx, method, path, body)
}

func SendManagerRequestWithCallback(ctx context.Context, method, path string, body map[string]interface{}, callback func(*WebSocketResponse)) error {
	return GetDefaultClient().SendRequestWithCallback(ctx, method, path, body, callback)
}

func GetManagerResponse(requestID string, timeout time.Duration) (*WebSocketResponse, error) {
	return GetDefaultClient().GetResponse(requestID, timeout)
}

// 双向通信支持方法
func SetManagerRequestHandler(handler func(*WebSocketRequest)) {
	GetDefaultClient().SetRequestHandler(handler)
}

func SendManagerResponse(requestID string, status int, body map[string]interface{}, errorMsg string) error {
	return GetDefaultClient().SendResponse(requestID, status, body, errorMsg)
}

// 创建带有请求处理器的客户端
func NewManagerClientWithHandler(handler func(*WebSocketRequest)) *WebSocketClient {
	return NewWebSocketClientWithHandler(handler)
}

// SendMcpToolListRequest 发送MCP工具列表请求
func SendMcpToolListRequest(ctx context.Context, agentID string) (*WebSocketResponse, error) {
	body := map[string]interface{}{
		"agent_id": agentID,
	}
	return SendManagerRequest(ctx, "GET", "/api/mcp/tools", body)
}

// SendMcpToolListRequestAsync 异步发送MCP工具列表请求
func SendMcpToolListRequestAsync(ctx context.Context, agentID string) (string, error) {
	body := map[string]interface{}{
		"agent_id": agentID,
	}
	return SendManagerRequestAsync(ctx, "GET", "/api/mcp/tools", body)
}

// SendMcpToolListRequestWithCallback 使用回调发送MCP工具列表请求
func SendMcpToolListRequestWithCallback(ctx context.Context, agentID string, callback func(*WebSocketResponse)) error {
	body := map[string]interface{}{
		"agent_id": agentID,
	}
	return SendManagerRequestWithCallback(ctx, "GET", "/api/mcp/tools", body, callback)
}

// Init 初始化Manager配置提供者
// 包括WebSocket连接的初始化和重连机制
func Init(ctx context.Context) error {
	log.Infof("Initializing Manager config provider with WebSocket client")

	// 创建WebSocket客户端
	client := GetDefaultClient()

	// 尝试连接到WebSocket服务器
	if err := client.Connect(ctx); err != nil {
		log.Warnf("初始连接Manager WebSocket失败: %v，将启动重连机制", err)
		// 即使初始连接失败，也启动重连机制
		client.triggerReconnect()
	} else {
		log.Infof("Manager config provider initialized successfully")
	}

	return nil
}

// Close 关闭Manager配置提供者，清理资源
func Close() error {
	log.Infof("Closing Manager config provider")

	// 停止重连协程
	client := GetDefaultClient()
	client.StopReconnect()

	// 主动断开连接（不触发重连）
	client.disconnect(true)

	return nil
}

// IsConnected 检查Manager配置提供者是否已连接
func IsConnected() bool {
	return IsManagerWebSocketConnected()
}

// handleMcpToolCallRequest 处理MCP工具调用请求
func (c *WebSocketClient) handleMcpToolCallRequest(request *WebSocketRequest) {
	agentID := ""
	deviceID := ""
	toolName := ""
	arguments := map[string]interface{}{}
	if request.Body != nil {
		if id, ok := request.Body["agent_id"].(string); ok {
			agentID = id
		}
		if id, ok := request.Body["device_id"].(string); ok {
			deviceID = id
		}
		if t, ok := request.Body["tool_name"].(string); ok {
			toolName = t
		}
		if args, ok := request.Body["arguments"].(map[string]interface{}); ok {
			arguments = args
		}
	}

	if toolName == "" || (agentID == "" && deviceID == "") {
		_ = c.SendResponse(request.ID, 400, nil, "缺少tool_name或agent_id/device_id参数")
		return
	}

	if agentID != "" && deviceID != "" {
		_ = c.SendResponse(request.ID, 400, nil, "agent_id与device_id不能同时传入")
		return
	}

	var (
		invokable tool.InvokableTool
		ok        bool
	)
	if deviceID != "" {
		invokable, ok = mcp.GetReportedToolByDeviceIDAndName(deviceID, toolName)
	} else {
		invokable, ok = mcp.GetReportedToolByAgentIDAndName(agentID, toolName)
	}
	if !ok {
		_ = c.SendResponse(request.ID, 404, nil, fmt.Sprintf("工具不存在: %s", toolName))
		return
	}

	argBytes, _ := json.Marshal(arguments)
	result, err := invokable.InvokableRun(context.Background(), string(argBytes))
	if err != nil {
		_ = c.SendResponse(request.ID, 500, nil, fmt.Sprintf("工具调用失败: %v", err))
		return
	}

	_ = c.SendResponse(request.ID, 200, map[string]interface{}{
		"agent_id":  agentID,
		"device_id": deviceID,
		"tool_name": toolName,
		"result":    result,
	}, "")
}
