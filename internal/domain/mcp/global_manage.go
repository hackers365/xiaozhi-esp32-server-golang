package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"

	log "xiaozhi-esp32-server-golang/logger"
)

const (
	globalMCPPingInterval                 = 60 * time.Second
	globalMCPPingTimeout                  = 5 * time.Second
	globalMCPRequestTimeout               = 15 * time.Second
	globalMCPPeriodicToolsRefreshInterval = 2 * time.Minute
)

var errMCPRequestBusy = errors.New("mcp request already in progress")

// MCPServerConfig MCP服务器配置
type MCPServerConfig struct {
	Name         string            `json:"name" mapstructure:"name"`
	Type         string            `json:"type" mapstructure:"type"`
	Url          string            `json:"url" mapstructure:"url"`
	SSEUrl       string            `json:"sse_url" mapstructure:"sse_url"` // 向后兼容 sse_url 字段
	Enabled      bool              `json:"enabled" mapstructure:"enabled"`
	Provider     string            `json:"provider,omitempty" mapstructure:"provider"`
	ServiceID    string            `json:"service_id,omitempty" mapstructure:"service_id"`
	AuthRef      string            `json:"auth_ref,omitempty" mapstructure:"auth_ref"`
	Headers      map[string]string `json:"headers,omitempty" mapstructure:"headers"`
	AllowedTools []string          `json:"allowed_tools,omitempty" mapstructure:"allowed_tools"`
}

// GlobalMCPManager 全局MCP管理器
type GlobalMCPManager struct {
	servers       map[string]*MCPServerConnection
	tools         map[string]tool.InvokableTool
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	reconnectConf ReconnectConfig
	httpClient    *http.Client
}

// ReconnectConfig 重连配置
type ReconnectConfig struct {
	Interval    time.Duration
	MaxAttempts int
}

// MCPServerConnection MCP服务器连接
type MCPServerConnection struct {
	config        MCPServerConfig
	client        *client.Client
	tools         map[string]tool.InvokableTool
	connected     bool
	refreshing    bool
	refreshQueued bool
	pinging       bool
	requestMu     sync.Mutex
	mu            sync.RWMutex
	lastError     error
	retryCount    int
	lastPing      time.Time
	reconnecting  bool
	reconnectWait chan struct{}
}

var (
	globalManager *GlobalMCPManager
	once          sync.Once
)

var buildGlobalMCPTransport = buildMCPTransport

// GetGlobalMCPManager 获取全局MCP管理器单例
func GetGlobalMCPManager() *GlobalMCPManager {
	once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalManager = &GlobalMCPManager{
			servers: make(map[string]*MCPServerConnection),
			tools:   make(map[string]tool.InvokableTool),
			ctx:     ctx,
			cancel:  cancel,
			reconnectConf: ReconnectConfig{
				Interval:    time.Duration(viper.GetInt("mcp.global.reconnect_interval")) * time.Second,
				MaxAttempts: viper.GetInt("mcp.global.max_reconnect_attempts"),
			},
			httpClient: &http.Client{
				Timeout: 600 * time.Second,
			},
		}
	})
	return globalManager
}

// Start 启动全局MCP管理器
func (g *GlobalMCPManager) Start() error {
	// 热更场景：Stop 后 ctx 已取消，需重建以便重启后监控与重连正常
	if g.ctx != nil && g.ctx.Err() != nil {
		g.ctx, g.cancel = context.WithCancel(context.Background())
		g.reconnectConf = ReconnectConfig{
			Interval:    time.Duration(viper.GetInt("mcp.global.reconnect_interval")) * time.Second,
			MaxAttempts: viper.GetInt("mcp.global.max_reconnect_attempts"),
		}
	}

	// 首先检查配置
	CheckMCPConfig()

	if !viper.GetBool("mcp.global.enabled") {
		log.Info("全局MCP管理器已禁用")
		return nil
	}

	var serverConfigs []MCPServerConfig
	if err := viper.UnmarshalKey("mcp.global.servers", &serverConfigs); err != nil {
		log.Errorf("解析MCP服务器配置失败: %v", err)
		return fmt.Errorf("解析MCP服务器配置失败: %v", err)
	}

	log.Infof("从配置中读取到 %d 个MCP服务器配置", len(serverConfigs))

	// 详细记录每个服务器配置
	for i, config := range serverConfigs {
		log.Infof("MCP服务器[%d]: Type=%s, Name=%s, Url=%s, SSEUrl=%s, Enabled=%v",
			i+1, config.Type, config.Name, config.Url, config.SSEUrl, config.Enabled)
	}

	// 连接启用的服务器
	connectedCount := 0
	for _, config := range serverConfigs {
		if config.Enabled {
			if err := g.connectToServer(config); err != nil {
				log.Errorf("连接到MCP服务器 %s 失败: %v", config.Name, err)
			} else {
				connectedCount++
			}
		} else {
			log.Infof("MCP服务器 %s 已禁用，跳过连接", config.Name)
		}
	}

	log.Infof("成功连接了 %d 个MCP服务器", connectedCount)

	// 启动监控goroutine
	go g.monitorConnections()

	log.Info("全局MCP管理器已启动")
	return nil
}

// Stop 停止全局MCP管理器
func (g *GlobalMCPManager) Stop() error {
	g.cancel()

	g.mu.Lock()
	type serverEntry struct {
		name string
		conn *MCPServerConnection
	}
	servers := make([]serverEntry, 0, len(g.servers))
	for name, conn := range g.servers {
		if conn != nil {
			servers = append(servers, serverEntry{name: name, conn: conn})
		}
	}
	g.servers = make(map[string]*MCPServerConnection)
	g.tools = make(map[string]tool.InvokableTool)
	g.mu.Unlock()

	for _, server := range servers {
		if err := server.conn.disconnect(); err != nil {
			log.Errorf("断开MCP服务器 %s 连接失败: %v", server.name, err)
		}
	}

	log.Info("全局MCP管理器已停止")
	return nil
}

// createFailedConnection 创建失败的连接对象用于后续重连
func (g *GlobalMCPManager) createFailedConnection(config MCPServerConfig) {
	conn := &MCPServerConnection{
		config:     config,
		tools:      make(map[string]tool.InvokableTool),
		connected:  false,
		lastError:  fmt.Errorf("初始化连接失败"),
		retryCount: 0,
	}

	g.mu.Lock()
	g.servers[config.Name] = conn
	g.mu.Unlock()

	log.Infof("已为失败的MCP服务器创建连接对象: %s", config.Name)
}

// connectToServer 连接到MCP服务器
func (g *GlobalMCPManager) connectToServer(config MCPServerConfig) error {
	// 验证配置
	if config.Name == "" {
		return fmt.Errorf("MCP服务器名称不能为空")
	}

	if !config.Enabled {
		log.Infof("MCP服务器 %s 已禁用，跳过连接", config.Name)
		return nil
	}

	_, endpoint, endpointErr := endpointForConfig(config)
	if endpointErr != nil {
		return endpointErr
	}
	log.Infof("正在连接MCP服务器: %s (URL: %s)", config.Name, endpoint)

	conn := &MCPServerConnection{
		config: config,
		tools:  make(map[string]tool.InvokableTool),
	}

	g.mu.Lock()
	g.servers[config.Name] = conn
	g.mu.Unlock()

	// 连接到服务器
	if err := conn.connect(); err != nil {
		return fmt.Errorf("连接MCP服务器失败: %v", err)
	}

	log.Infof("已连接到MCP服务器: %s", config.Name)
	return nil
}

// connect 连接到MCP服务器
func (conn *MCPServerConnection) connect() (retErr error) {
	// 使用背景上下文，不设置超时，让SSE连接长期保持
	ctx := context.Background()

	transportInstance, endpoint, err := buildGlobalMCPTransport(conn.config)
	if err != nil {
		return err
	}

	// 使用 client.NewClient 创建 MCP 客户端
	mcpClient := client.NewClient(transportInstance)
	serverName := conn.config.Name
	defer func() {
		if retErr == nil {
			return
		}

		conn.mu.Lock()
		conn.client = nil
		conn.connected = false
		conn.refreshing = false
		conn.refreshQueued = false
		conn.tools = make(map[string]tool.InvokableTool)
		conn.lastError = retErr
		conn.mu.Unlock()

		if globalManager != nil {
			globalManager.removeGlobalTools(serverName)
		}

		if closeErr := mcpClient.Close(); closeErr != nil {
			log.Errorf("关闭MCP客户端失败: %v", closeErr)
		}
	}()

	mcpClient.OnNotification(conn.handleJSONRPCNotification)
	conn.mu.Lock()
	conn.client = mcpClient
	conn.mu.Unlock()

	log.Infof("开始连接MCP服务器: %s, %s URL: %s", conn.config.Name, conn.config.Type, endpoint)

	// 启动客户端
	if err := mcpClient.Start(ctx); err != nil {
		log.Errorf("启动MCP客户端失败，服务器: %s, 错误: %v", conn.config.Name, err)
		retErr = fmt.Errorf("启动客户端失败: %v", err)
		return retErr
	}

	log.Infof("MCP客户端启动成功: %s", conn.config.Name)

	// 初始化客户端
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "xiaozhi-esp32-server",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{
				Experimental: make(map[string]any),
			},
		},
	}

	log.Infof("正在初始化MCP服务器: %s", conn.config.Name)
	initCtx, initCancel := context.WithTimeout(ctx, globalMCPRequestTimeout)
	initResult, err := mcpClient.Initialize(initCtx, initRequest)
	initCancel()
	if err != nil {
		log.Errorf("初始化MCP服务器失败，服务器: %s, 错误: %v", conn.config.Name, err)
		retErr = fmt.Errorf("初始化失败: %v", err)
		return retErr
	}

	log.Infof("MCP服务器初始化成功: %s, 结果: %+v", conn.config.Name, initResult)

	// 获取工具列表
	refreshCtx, refreshCancel := context.WithTimeout(ctx, globalMCPRequestTimeout)
	refreshErr := conn.refreshTools(refreshCtx)
	refreshCancel()
	if refreshErr != nil {
		log.Errorf("获取工具列表失败: %v", refreshErr)
		retErr = fmt.Errorf("获取工具列表失败: %v", refreshErr)
		return retErr
	}

	conn.mu.Lock()
	conn.connected = true
	conn.lastError = nil
	conn.retryCount = 0
	conn.mu.Unlock()

	log.Infof("MCP服务器连接建立完成: %s", conn.config.Name)
	return nil
}

func (conn *MCPServerConnection) handleJSONRPCNotification(notification mcp.JSONRPCNotification) {
	switch notification.Method {
	case mcp.MethodNotificationToolsListChanged, "notifications/tools/updated":
		log.Infof("MCP服务器 %s 收到工具列表更新通知，准备刷新工具列表", conn.config.Name)
		conn.scheduleToolsRefresh()
	}
}

func (conn *MCPServerConnection) scheduleToolsRefresh() {
	conn.scheduleToolsRefreshWithReason("基于通知")
}

func (conn *MCPServerConnection) schedulePeriodicToolsRefresh() {
	conn.scheduleToolsRefreshWithReason("周期")
}

func (conn *MCPServerConnection) scheduleToolsRefreshWithReason(reason string) {
	conn.mu.Lock()
	if conn.refreshing {
		conn.refreshQueued = true
		conn.mu.Unlock()
		return
	}
	conn.refreshing = true
	conn.mu.Unlock()

	go conn.runScheduledToolsRefresh(reason)
}

func (conn *MCPServerConnection) runScheduledToolsRefresh(reason string) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := conn.refreshTools(ctx)
		cancel()
		if err != nil {
			log.Warnf("MCP服务器 %s %s刷新工具列表失败: %v", conn.config.Name, reason, err)
		}

		conn.mu.Lock()
		if err != nil {
			conn.lastError = err
		} else {
			conn.lastError = nil
		}

		if conn.refreshQueued {
			conn.refreshQueued = false
			conn.mu.Unlock()
			continue
		}

		conn.refreshing = false
		conn.mu.Unlock()
		return
	}
}

func normalizeMCPTransportType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "sse":
		return "sse"
	case "streamable_http", "streamable-http", "http":
		return "streamablehttp"
	default:
		return strings.ToLower(strings.TrimSpace(t))
	}
}

func endpointForConfig(config MCPServerConfig) (string, string, error) {
	transportType := normalizeMCPTransportType(config.Type)
	if transportType == "" {
		if strings.TrimSpace(config.SSEUrl) != "" {
			transportType = "sse"
		} else if strings.TrimSpace(config.Url) != "" {
			transportType = "streamablehttp"
		}
	}

	switch transportType {
	case "sse":
		if strings.TrimSpace(config.SSEUrl) != "" {
			return transportType, strings.TrimSpace(config.SSEUrl), nil
		}
		if strings.TrimSpace(config.Url) != "" {
			return transportType, strings.TrimSpace(config.Url), nil
		}
		return "", "", fmt.Errorf("MCP服务器 %s 缺少SSE URL", config.Name)
	case "streamablehttp":
		if strings.TrimSpace(config.Url) != "" {
			return transportType, strings.TrimSpace(config.Url), nil
		}
		if strings.TrimSpace(config.SSEUrl) != "" {
			return transportType, strings.TrimSpace(config.SSEUrl), nil
		}
		return "", "", fmt.Errorf("MCP服务器 %s 缺少StreamableHTTP URL", config.Name)
	default:
		return "", "", fmt.Errorf("MCP服务器 %s 类型不支持: %s", config.Name, config.Type)
	}
}

func buildMCPTransport(config MCPServerConfig) (transport.Interface, string, error) {
	transportType, endpoint, err := endpointForConfig(config)
	if err != nil {
		return nil, "", err
	}

	headers := make(map[string]string)
	for k, v := range config.Headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		headers[strings.TrimSpace(k)] = v
	}

	switch transportType {
	case "sse":
		opts := make([]transport.ClientOption, 0)
		if len(headers) > 0 {
			opts = append(opts, transport.WithHeaders(headers))
		}
		sseTransport, err := transport.NewSSE(endpoint, opts...)
		if err != nil {
			return nil, "", fmt.Errorf("创建SSE传输层失败: %v", err)
		}
		return sseTransport, endpoint, nil
	case "streamablehttp":
		opts := make([]transport.StreamableHTTPCOption, 0)
		if len(headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(headers))
		}
		httpTransport, err := transport.NewStreamableHTTP(endpoint, opts...)
		if err != nil {
			return nil, "", fmt.Errorf("创建StreamableHTTP传输层失败: %v", err)
		}
		return httpTransport, endpoint, nil
	default:
		return nil, "", fmt.Errorf("不支持的MCP传输类型: %s", transportType)
	}
}

func buildAllowedToolSet(allowedTools []string) map[string]struct{} {
	if len(allowedTools) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(allowedTools))
	for _, toolName := range allowedTools {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}
		set[toolName] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func filterMCPToolsByAllowList(tools []mcp.Tool, allowedTools []string) []mcp.Tool {
	allowedSet := buildAllowedToolSet(allowedTools)
	if len(allowedSet) == 0 {
		return tools
	}

	filtered := make([]mcp.Tool, 0, len(tools))
	for _, item := range tools {
		if _, ok := allowedSet[strings.TrimSpace(item.Name)]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// refreshTools 刷新工具列表
func (conn *MCPServerConnection) refreshTools(ctx context.Context) error {
	conn.mu.RLock()
	serverName := conn.config.Name
	allowedTools := append([]string(nil), conn.config.AllowedTools...)
	mcpClient := conn.client
	conn.mu.RUnlock()
	if mcpClient == nil {
		return fmt.Errorf("MCP客户端未初始化")
	}

	conn.requestMu.Lock()
	defer conn.requestMu.Unlock()

	// 获取工具列表
	listRequest := mcp.ListToolsRequest{}
	toolsResult, err := mcpClient.ListTools(ctx, listRequest)
	if err != nil {
		return fmt.Errorf("获取工具列表失败: %v", err)
	}

	tools := filterMCPToolsByAllowList(toolsResult.Tools, allowedTools)
	convertedTools := ConvertMcpToolListToInvokableToolList(tools, serverName, mcpClient, &conn.requestMu)

	conn.mu.Lock()
	conn.tools = convertedTools
	conn.mu.Unlock()

	// 全局工具表的更新放在 conn.mu 外，避免与 g.mu 形成锁顺序反转。
	globalManager.updateGlobalTools(serverName, convertedTools)

	log.Infof("MCP服务器 %s 工具列表已更新，共 %d 个工具", serverName, len(convertedTools))
	return nil
}

func ConvertMcpToolListToInvokableToolList(tools []mcp.Tool, serverName string, client *client.Client, requestMu ...*sync.Mutex) map[string]tool.InvokableTool {
	invokeTools := make(map[string]tool.InvokableTool)
	usedNames := make(map[string]string, len(tools))
	var sharedRequestMu *sync.Mutex
	if len(requestMu) > 0 {
		sharedRequestMu = requestMu[0]
	}
	for _, tool := range tools {
		originName := tool.Name
		if strings.TrimSpace(originName) == "" {
			log.Warnf("跳过空名称 MCP 工具, server=%s", serverName)
			continue
		}
		llmName := uniqueLLMToolName(sanitizeLLMToolName(originName), originName, usedNames)
		if llmName != originName {
			log.Debugf("MCP工具名 %q 不符合OpenAI工具名规范，已转换为 %q, server=%s", originName, llmName, serverName)
		}

		marshaledInputSchema, err := sonic.Marshal(tool.InputSchema)
		if err != nil {
			log.Errorf("convert mcp tool to invokeable tool err: %+v", err)
			continue
		}
		inputSchema := &openapi3.Schema{}
		err = sonic.Unmarshal(marshaledInputSchema, inputSchema)
		if err != nil {
			log.Errorf("convert mcp tool to invokeable tool err: %+v", err)
			continue
		}

		mcpToolInstance := &McpTool{
			info: &schema.ToolInfo{
				Name:        llmName,
				Desc:        tool.Description,
				ParamsOneOf: schema.NewParamsOneOfByOpenAPIV3(inputSchema),
			},
			originName: originName,
			serverName: serverName,
			client:     client,
			requestMu:  sharedRequestMu,
		}
		invokeTools[llmName] = mcpToolInstance
	}
	return invokeTools
}

// disconnect 断开连接
func (conn *MCPServerConnection) disconnect() error {
	conn.mu.Lock()
	serverName := conn.config.Name
	mcpClient := conn.client
	conn.client = nil
	conn.connected = false
	conn.tools = make(map[string]tool.InvokableTool)
	conn.mu.Unlock()

	if globalManager != nil {
		globalManager.removeGlobalTools(serverName)
	}

	if mcpClient != nil {
		// 关闭客户端放在锁外，避免锁住快路径。
		if err := mcpClient.Close(); err != nil {
			log.Errorf("关闭MCP客户端失败: %v", err)
		}
	}

	return nil
}

func (g *GlobalMCPManager) removeGlobalTools(serverName string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for name, mcpToolInterface := range g.tools {
		if mt, ok := mcpToolInterface.(*McpTool); ok && mt.serverName == serverName {
			delete(g.tools, name)
		}
	}
}

// updateGlobalTools 更新全局工具列表
func (g *GlobalMCPManager) updateGlobalTools(serverName string, tools map[string]tool.InvokableTool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 移除该服务器的旧工具
	for name, mcpToolInterface := range g.tools {
		if mt, ok := mcpToolInterface.(*McpTool); ok && mt.serverName == serverName {
			delete(g.tools, name)
		}
	}

	// 添加新工具
	for name, mcpToolInterface := range tools {
		g.tools[fmt.Sprintf("%s_%s", serverName, name)] = mcpToolInterface
	}
}

// GetAllTools 获取所有可用工具
func (g *GlobalMCPManager) GetAllTools() map[string]tool.InvokableTool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make(map[string]tool.InvokableTool)
	for name, mcpToolInterface := range g.tools {
		result[name] = mcpToolInterface
	}
	return result
}

// GetToolByName 根据名称获取工具
func (g *GlobalMCPManager) GetToolByName(name string) (tool.InvokableTool, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if invokable, exists := g.tools[name]; exists {
		return invokable, true
	}

	var matched tool.InvokableTool
	matchCount := 0
	for _, invokable := range g.tools {
		if !mcpToolMatchesName(invokable, name) {
			continue
		}
		matchCount++
		if matchCount == 1 {
			matched = invokable
			continue
		}

		log.Warnf("全局MCP工具名 %s 存在多个同名提供方，请显式指定 server 名称", name)
		return nil, false
	}
	return matched, matchCount == 1
}

func GetServerClientByName(serverName string) *client.Client {
	return GetGlobalMCPManager().GetServerClientByName(serverName)
}

func (g *GlobalMCPManager) GetServerClientByName(serverName string) *client.Client {
	g.mu.RLock()
	conn, ok := g.servers[serverName]
	g.mu.RUnlock()
	if !ok || conn == nil {
		return nil
	}

	conn.mu.RLock()
	defer conn.mu.RUnlock()
	return conn.client
}

func GetServerEndpointSnapshotByName(serverName string) string {
	return GetGlobalMCPManager().GetServerEndpointSnapshotByName(serverName)
}

func (g *GlobalMCPManager) GetServerEndpointSnapshotByName(serverName string) string {
	g.mu.RLock()
	conn, ok := g.servers[serverName]
	g.mu.RUnlock()
	if !ok || conn == nil {
		return ""
	}

	conn.mu.RLock()
	config := conn.config
	conn.mu.RUnlock()

	_, endpoint, err := endpointForConfig(config)
	if err != nil {
		if strings.TrimSpace(config.Url) != "" {
			return strings.TrimSpace(config.Url)
		}
		return strings.TrimSpace(config.SSEUrl)
	}
	return endpoint
}

func ReconnectServerByName(serverName string) (*client.Client, error) {
	return GetGlobalMCPManager().reconnectServer(serverName)
}

// isSessionClosedError 判断是否为session closed错误
func isSessionClosedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "session closed")
}

func isRetryableRemoteCallError(err error) bool {
	if err == nil {
		return false
	}
	if isSessionClosedError(err) {
		return true
	}

	message := strings.ToLower(err.Error())
	retryableIndicators := []string{
		"unexpected end of json input",
		"invalid character",
		"eof",
		"broken pipe",
		"connection reset",
		"connection refused",
		"connection aborted",
		"timeout",
		"bad gateway",
		"502",
		"temporarily unavailable",
	}
	for _, indicator := range retryableIndicators {
		if strings.Contains(message, indicator) {
			return true
		}
	}
	return false
}

func (g *GlobalMCPManager) schedulePeriodicToolsRefresh() {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, conn := range g.servers {
		if conn == nil {
			continue
		}

		conn.mu.RLock()
		connected := conn.connected
		hasClient := conn.client != nil
		conn.mu.RUnlock()
		if !connected || !hasClient {
			continue
		}

		conn.schedulePeriodicToolsRefresh()
	}
}

// monitorConnections 监控连接状态
func (g *GlobalMCPManager) monitorConnections() {
	pingTicker := time.NewTicker(globalMCPPingInterval) // 每60秒ping一次
	defer pingTicker.Stop()
	toolsRefreshTicker := time.NewTicker(globalMCPPeriodicToolsRefreshInterval)
	defer toolsRefreshTicker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-pingTicker.C:
			// 执行ping检测
			g.mu.RLock()
			for name, conn := range g.servers {
				if conn == nil || !conn.tryBeginPing() {
					continue
				}
				go func(name string, conn *MCPServerConnection) {
					defer conn.finishPing()
					ctx, cancel := context.WithTimeout(context.Background(), globalMCPPingTimeout)
					defer cancel()

					if err := conn.ping(ctx); err != nil {
						if errors.Is(err, errMCPRequestBusy) {
							log.Debugf("MCP服务器 %s 正在处理其他请求，跳过本轮ping", name)
							return
						}
						log.Warnf("MCP服务器 %s ping失败，开始重连: %v", name, err)
						// ping失败时直接标记为断开并触发重连
						conn.mu.Lock()
						conn.connected = false
						conn.lastError = err
						conn.mu.Unlock()

						// 直接触发重连
						go g.reconnectServer(name)
					} else {
						//log.Debugf("MCP服务器 %s ping成功", name)
					}
				}(name, conn)
			}
			g.mu.RUnlock()
		case <-toolsRefreshTicker.C:
			g.schedulePeriodicToolsRefresh()
		}
	}
}

// reconnectServer 重连服务器并返回新的client
func (g *GlobalMCPManager) reconnectServer(serverName string) (*client.Client, error) {
	g.mu.RLock()
	var conn *MCPServerConnection
	for _, c := range g.servers {
		if c.config.Name == serverName {
			conn = c
			break
		}
	}
	g.mu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("未找到服务器连接: %s", serverName)
	}

	conn.mu.Lock()
	if conn.reconnecting {
		wait := conn.reconnectWait
		conn.mu.Unlock()
		if wait != nil {
			<-wait
		}

		conn.mu.RLock()
		mcpClient := conn.client
		connected := conn.connected
		lastErr := conn.lastError
		conn.mu.RUnlock()
		if mcpClient != nil && connected {
			return mcpClient, nil
		}
		if lastErr != nil {
			return nil, fmt.Errorf("重连失败: %v", lastErr)
		}
		return nil, fmt.Errorf("重连失败: client未就绪")
	}
	wait := make(chan struct{})
	conn.reconnecting = true
	conn.reconnectWait = wait
	conn.mu.Unlock()

	defer func() {
		conn.mu.Lock()
		conn.reconnecting = false
		if conn.reconnectWait == wait {
			close(wait)
			conn.reconnectWait = nil
		}
		conn.mu.Unlock()
	}()

	// 断开连接
	if err := conn.disconnect(); err != nil {
		log.Errorf("断开连接失败: %v", err)
	}

	// 等待一小段时间确保资源释放
	time.Sleep(time.Second)

	// 重新连接
	if err := conn.connect(); err != nil {
		conn.mu.Lock()
		conn.lastError = err
		conn.mu.Unlock()
		return nil, fmt.Errorf("重连失败: %v", err)
	}

	conn.mu.RLock()
	mcpClient := conn.client
	conn.mu.RUnlock()
	return mcpClient, nil
}

func (conn *MCPServerConnection) tryBeginPing() bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.pinging || conn.reconnecting || conn.client == nil {
		return false
	}
	conn.pinging = true
	return true
}

func (conn *MCPServerConnection) finishPing() {
	conn.mu.Lock()
	conn.pinging = false
	conn.mu.Unlock()
}

// ping 发送ping请求检测连接状态
func (conn *MCPServerConnection) ping(ctx context.Context) error {
	conn.mu.RLock()
	mcpClient := conn.client
	conn.mu.RUnlock()
	if mcpClient == nil {
		return fmt.Errorf("client未初始化")
	}

	if !conn.requestMu.TryLock() {
		return errMCPRequestBusy
	}
	defer conn.requestMu.Unlock()

	// 使用空的Ping请求作为ping
	err := mcpClient.Ping(ctx)
	if err != nil {
		return fmt.Errorf("ping失败: %v", err)
	}

	conn.mu.Lock()
	conn.lastPing = time.Now()
	conn.mu.Unlock()

	return nil
}
