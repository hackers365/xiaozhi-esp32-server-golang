package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"xiaozhi-esp32-server-golang/internal/app/mqtt_server"
	"xiaozhi-esp32-server-golang/internal/app/server/chat"
	"xiaozhi-esp32-server-golang/internal/app/server/mqtt_udp"
	"xiaozhi-esp32-server-golang/internal/app/server/notify"
	"xiaozhi-esp32-server-golang/internal/app/server/types"
	"xiaozhi-esp32-server-golang/internal/app/server/websocket"
	"xiaozhi-esp32-server-golang/internal/data/history"
	msg_types "xiaozhi-esp32-server-golang/internal/data/msg"
	db_redis "xiaozhi-esp32-server-golang/internal/db/redis"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	config_types "xiaozhi-esp32-server-golang/internal/domain/config/types"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/domain/openclaw"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/pool"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/spf13/viper"
)

// App 统一管理所有协议服务和 ChatManager

type App struct {
	wsServer       *websocket.WebSocketServer
	mqttUdpAdapter *mqtt_udp.MqttUdpAdapter
	mqttUdpMu      sync.RWMutex

	// ChatManager管理 - 使用concurrent map
	chatManagers  cmap.ConcurrentMap[string, *chat.ChatManager]
	notifyService *notify.NotifyService
}

func NewApp() *App {
	var err error
	app := &App{
		chatManagers: cmap.New[*chat.ChatManager](),
	}
	app.initNotifyService()
	mcp.RegisterCurrentDeviceTransportResolver(func(deviceID string) string {
		chatManager, exists := app.GetChatManager(deviceID)
		if !exists || chatManager == nil {
			return ""
		}
		return chatManager.GetTransportType()
	})
	app.wsServer = app.newWebSocketServer()
	app.mqttUdpAdapter, err = app.newMqttUdpAdapter()
	if err != nil {
		log.Errorf("newMqttUdpAdapter err: %+v", err)
		return nil
	}
	return app
}

func (a *App) Run() {
	go a.wsServer.Start()
	log.Infof("enter Run, mqtt_server.enable: %v", viper.GetBool("mqtt_server.enable"))
	if viper.GetBool("mqtt_server.enable") {
		go func() {
			err := a.startMqttServer()
			if err != nil {
				log.Errorf("startMqttServer err: %+v", err)
			}
		}()
	}
	a.mqttUdpMu.RLock()
	adapter := a.mqttUdpAdapter
	a.mqttUdpMu.RUnlock()
	if adapter != nil {
		go adapter.Start() // 非阻塞，连接与重试在 adapter 内部后台执行
	}

	// 注册聊天相关的本地MCP工具
	a.registerChatMCPTools()

	a.registerHandler()

	a.initEventHandle()

	// 启动资源池统计监控（每5分钟输出一次到日志）
	ctx := context.Background()
	pool.StartStatsMonitor(ctx, 5*time.Minute)

	// 启动资源池统计上报（每5秒上报一次到 manager backend）
	pool.StartStatsReporter(ctx)

	select {} // 阻塞主线程
}

func (app *App) initEventHandle() {
	eventHandle, err := NewEventHandle(app)
	if err != nil {
		log.Errorf("初始化 EventHandle 失败: %v", err)
		return
	}
	if err := eventHandle.Start(); err != nil {
		log.Errorf("启动 EventHandle 失败: %v", err)
		return
	}

	// 初始化消息处理器（总是启用，统一处理Redis+MemoryProvider+History）
	historyCfg := history.HistoryClientConfig{
		BaseURL:   util.GetBackendURL(),
		AuthToken: util.GetManagerAuthToken(),
		Timeout:   viper.GetDuration("manager.history_timeout"),
		Enabled:   true, // 总是启用
	}
	NewMessageWorker(historyCfg)
	log.Info("消息处理器已初始化")
}

func (app *App) currentMqttConfig() *mqtt_udp.MqttConfig {
	if !viper.GetBool("mqtt.enable") {
		return nil
	}
	return &mqtt_udp.MqttConfig{
		Broker:   viper.GetString("mqtt.broker"),
		Type:     viper.GetString("mqtt.type"),
		Port:     viper.GetInt("mqtt.port"),
		ClientID: viper.GetString("mqtt.client_id"),
		Username: viper.GetString("mqtt.username"),
		Password: viper.GetString("mqtt.password"),
	}
}

func (app *App) newMqttUdpAdapter() (*mqtt_udp.MqttUdpAdapter, error) {
	mqttConfig := app.currentMqttConfig()
	if mqttConfig == nil {
		return nil, nil
	}

	udpServer, err := app.newUdpServer()
	if err != nil {
		return nil, err
	}

	return mqtt_udp.NewMqttUdpAdapter(
		mqttConfig,
		mqtt_udp.WithUdpServer(udpServer),
		mqtt_udp.WithOnNewConnection(app.OnNewConnection),
		mqtt_udp.WithOnDeviceOnline(app.DeviceOnline),
		mqtt_udp.WithOnDeviceOffline(app.DeviceOffline),
		mqtt_udp.WithOnTransportReady(app.onMqttTransportReady),
		mqtt_udp.WithOfflineGracePeriod(app.mqttOfflineGracePeriod()),
	), nil
}

func (app *App) mqttOfflineGracePeriod() time.Duration {
	if duration := viper.GetDuration("mqtt.transport_offline_grace_period"); duration > 0 {
		return duration
	}
	if seconds := viper.GetInt("mqtt.transport_offline_grace_period_seconds"); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 2 * time.Minute
}

func (app *App) newUdpServer() (*mqtt_udp.UdpServer, error) {
	udpPort := viper.GetInt("udp.listen_port")
	externalHost := viper.GetString("udp.external_host")
	externalPort := viper.GetInt("udp.external_port")

	udpServer := mqtt_udp.NewUDPServer(udpPort, externalHost, externalPort)
	err := udpServer.Start()
	if err != nil {
		log.Fatalf("udpServer.Start err: %+v", err)
		return nil, err
	}
	return udpServer, nil
}

func (app *App) newWebSocketServer() *websocket.WebSocketServer {
	port := viper.GetInt("websocket.port")
	var notifyHandler http.HandlerFunc
	if app.notifyService != nil {
		notifyHandler = app.notifyService.HandleStream
	}
	return websocket.NewWebSocketServer(
		port,
		websocket.WithOnNewConnection(app.OnNewConnection),
		websocket.WithOnOpenClawResponse(app.OnOpenClawResponse),
		websocket.WithNotifyHandler(notifyHandler),
		websocket.WithOnInjectMsgHandler(func(ctx context.Context, body map[string]interface{}) (string, error) {
			return app.HandleInjectMsg(ctx, config_types.EventHandleMessageInject, body)
		}),
		websocket.WithOnInjectMessage(func(deviceID, message string, skipLlm bool, autoListen bool) error {
			chatManager, exists := app.GetChatManager(deviceID)
			if !exists || chatManager == nil {
				return fmt.Errorf("device %s not found or offline", deviceID)
			}
			return chatManager.InjectMessage(message, skipLlm, autoListen)
		}),
	)
}

func (app *App) initNotifyService() {
	secretKey := []byte(viper.GetString("manager.auth_token"))
	if len(secretKey) == 0 {
		secretKey = []byte(viper.GetString("internal_auth_token"))
	}
	publicBaseURL := viper.GetString("server.public_url")
	if publicBaseURL == "" {
		publicBaseURL = viper.GetString("notify.public_url")
	}
	if publicBaseURL == "" {
		publicBaseURL = fmt.Sprintf("http://127.0.0.1:%d", viper.GetInt("websocket.port"))
	}

	redisClient := db_redis.GetClient()

	ttsResolver := func(ctx context.Context, config *notify.NotifyConfig) (notify.TTSStreamProvider, func(), error) {
		chatManager, exists := app.GetChatManager(config.DeviceID)
		var deviceConfig config_types.UConfig
		if exists && chatManager != nil && chatManager.GetClientState() != nil {
			deviceConfig = chatManager.GetClientState().DeviceConfig
		}

		ttsConfig := make(map[string]interface{})
		for k, v := range deviceConfig.Tts.Config {
			ttsConfig[k] = v
		}
		ttsProvider := deviceConfig.Tts.Provider
		if ttsProvider == "" {
			ttsProvider = viper.GetString("tts.provider")
			if ttsProvider == "" {
				ttsProvider = "edge"
			}
		}

		if config.Voice != "" {
			ttsConfig["voice"] = config.Voice
			ttsConfig["spk_id"] = config.Voice
		}
		if config.Speed > 0 {
			ttsConfig["speed"] = config.Speed
		}

		providerLabel := ttsProvider
		if config.Voice != "" {
			providerLabel = fmt.Sprintf("%s:%s", ttsProvider, config.Voice)
		}

		ttsWrapper, err := pool.Acquire[tts.TTSProvider]("tts", providerLabel, ttsConfig)
		if err == nil && ttsWrapper != nil {
			return ttsWrapper.GetResource(), ttsWrapper.Release, nil
		}

		p, err := tts.GetTTSProvider(providerLabel, ttsConfig)
		if err != nil {
			return nil, nil, err
		}
		return p, func() { _ = p.Close() }, nil
	}

	app.notifyService = notify.NewNotifyService(secretKey, publicBaseURL, redisClient, ttsResolver)
}

func (app *App) startMqttServer() error {
	return mqtt_server.StartMqttServer()
}

// ReloadMqttServer 热更 MQTT Server：先停，再根据 mqtt_server.enable 决定是否启动（未启用则仅停止不启动）
func (app *App) ReloadMqttServer() {
	_ = mqtt_server.StopMqttServer()
	if !viper.GetBool("mqtt_server.enable") {
		return
	}
	if err := app.startMqttServer(); err != nil {
		log.Errorf("ReloadMqttServer start: %v", err)
	}
}

// ReloadMqttUdp 热更 MQTT+UDP：先停旧适配器，再根据 mqtt.enable 决定是否新建并启动（未启用则仅停止不启动）
func (app *App) ReloadMqttUdp() {
	app.mqttUdpMu.Lock()
	old := app.mqttUdpAdapter
	app.mqttUdpAdapter = nil
	app.mqttUdpMu.Unlock()
	if old != nil {
		old.Stop()
	}
	if !viper.GetBool("mqtt.enable") {
		return
	}
	adapter, err := app.newMqttUdpAdapter()
	if err != nil {
		log.Errorf("ReloadMqttUdp newMqttUdpAdapter: %v", err)
		return
	}
	app.mqttUdpMu.Lock()
	app.mqttUdpAdapter = adapter
	app.mqttUdpMu.Unlock()
	time.Sleep(500 * time.Millisecond)
	go adapter.Start()
}

// ReloadMqttUdpWithFlags 根据变更标记决定是否热更 MQTT+UDP
func (app *App) ReloadMqttUdpWithFlags(doMqttReload, doUdpReload bool) {
	if !doMqttReload && !doUdpReload {
		return
	}
	if !viper.GetBool("mqtt.enable") {
		log.Infof("ReloadMqttUdpWithFlags: mqtt disabled, stopping mqtt+udp")
		app.ReloadMqttUdp()
		return
	}

	app.mqttUdpMu.RLock()
	adapter := app.mqttUdpAdapter
	app.mqttUdpMu.RUnlock()

	if adapter == nil {
		log.Infof("ReloadMqttUdpWithFlags: mqtt enabled but adapter is nil, starting mqtt+udp")
		newAdapter, err := app.newMqttUdpAdapter()
		if err != nil {
			log.Errorf("ReloadMqttUdpWithFlags newMqttUdpAdapter: %v", err)
			return
		}
		if newAdapter == nil {
			return
		}
		app.mqttUdpMu.Lock()
		app.mqttUdpAdapter = newAdapter
		app.mqttUdpMu.Unlock()
		time.Sleep(500 * time.Millisecond)
		go newAdapter.Start()
		return
	}

	if doMqttReload && doUdpReload {
		log.Infof("ReloadMqttUdpWithFlags: mqtt & udp config changed, reloading mqtt+udp")
		app.ReloadMqttUdp()
		return
	}
	if doMqttReload {
		log.Infof("ReloadMqttUdpWithFlags: mqtt config changed, reloading mqtt only")
		mqttConfig := app.currentMqttConfig()
		if mqttConfig == nil {
			app.ReloadMqttUdp()
			return
		}
		adapter.ReloadMqttClient(mqttConfig)
		return
	}
	if doUdpReload {
		log.Infof("ReloadMqttUdpWithFlags: udp listen changed, reloading udp only")
		udpServer, err := app.newUdpServer()
		if err != nil {
			log.Errorf("ReloadMqttUdpWithFlags newUdpServer: %v", err)
			return
		}
		adapter.ReloadUdpServer(udpServer)
	}
}

// ReloadMCP 热更 MCP：禁用时仅停止全局 MCP；启用时已启动则重启全局 MCP，未启动则启动 MCP 集群
func (app *App) ReloadMCP() error {
	if !viper.GetBool("mcp.global.enabled") {
		// 禁用：只停不启，避免依赖 Start() 内判断或合并时序
		if err := mcp.GetGlobalMCPManager().Stop(); err != nil {
			return err
		}
		return nil
	}
	mgr := mcp.GetMCPManager()
	if mgr.IsStarted() {
		if err := mgr.RestartManager("global"); err != nil {
			return err
		}
		return nil
	}
	if err := mcp.StartMCPManagers(); err != nil {
		return err
	}
	return nil
}

// 所有协议新连接都走这里
func (a *App) OnNewConnection(transport types.IConn) {
	deviceID := transport.GetDeviceID()
	transportType := transport.GetTransportType()
	notifyLifecycleOnManager := transportType != types.TransportTypeMqttUdp

	// 检查是否已存在该设备的ChatManager
	if existingManager, exists := a.chatManagers.Get(deviceID); exists {
		log.Infof("设备 %s 已存在ChatManager，先关闭旧的连接", deviceID)
		// 关闭旧的ChatManager
		existingManager.Close()
		a.chatManagers.Remove(deviceID)
	}

	// 创建新的ChatManager
	chatManager, err := chat.NewChatManager(deviceID, transport)
	if err != nil {
		log.Errorf("创建chatManager失败: %v", err)
		return
	}

	// 存储ChatManager
	a.chatManagers.Set(deviceID, chatManager)

	if notifyLifecycleOnManager {
		a.DeviceOnline(deviceID)
	}

	log.Infof("设备 %s 的ChatManager已创建并存储", deviceID)

	// OpenClaw离线消息补发（延迟重试，避免连接刚建立时会话尚未初始化）
	go a.replayOpenClawOfflineMessages(deviceID)

	// 启动ChatManager
	go func() {
		defer func() {
			// ChatManager结束时，从映射中移除
			if storedManager, exists := a.chatManagers.Get(deviceID); exists && storedManager == chatManager {
				a.chatManagers.Remove(deviceID)
				log.Infof("设备 %s 的ChatManager已从映射中移除", deviceID)
				if notifyLifecycleOnManager {
					a.DeviceOffline(deviceID)
				}
			}
		}()

		if err := chatManager.Start(); err != nil {
			log.Errorf("ChatManager启动失败: %v", err)
		}
	}()
}

func (a *App) onMqttTransportReady(deviceID string) {
	chatManager, exists := a.GetChatManager(deviceID)
	if !exists || chatManager == nil {
		return
	}
	chatManager.HandleMqttTransportReady()
	chatManager.WarmupMcp()
}

// OnOpenClawResponse OpenClaw实时响应下发回调
func (a *App) OnOpenClawResponse(event openclaw.ResponseDelivery) bool {
	deviceID := strings.TrimSpace(event.DeviceID)
	if deviceID == "" {
		return false
	}
	chatManager, exists := a.GetChatManager(deviceID)
	if !exists || chatManager == nil {
		return false
	}
	if err := chatManager.InjectOpenClawResponse(event); err != nil {
		log.Warnf(
			"OpenClaw实时消息注入失败, device=%s correlation_id=%s start=%v end=%v err=%v",
			deviceID,
			strings.TrimSpace(event.CorrelationID),
			event.IsStart,
			event.IsEnd,
			err,
		)
		return false
	}
	return true
}

func (a *App) replayOpenClawOfflineMessages(deviceID string) {
	manager := openclaw.GetManager()
	const maxRetry = 10
	for i := 0; i < maxRetry; i++ {
		time.Sleep(1 * time.Second)
		delivered, remaining := manager.ReplayOfflineMessages(deviceID, func(msg openclaw.OfflineMessage) error {
			chatManager, exists := a.GetChatManager(deviceID)
			if !exists || chatManager == nil {
				return fmt.Errorf("chat manager not ready")
			}
			if strings.TrimSpace(msg.Text) == "" {
				return nil
			}
			return chatManager.InjectMessage(msg.Text, true, false)
		})
		if delivered > 0 {
			log.Infof("OpenClaw离线消息补发成功, device=%s delivered=%d remaining=%d", deviceID, delivered, remaining)
		}
		if remaining == 0 {
			return
		}
	}
}

// GetChatManager 获取指定设备的ChatManager
func (a *App) GetChatManager(deviceID string) (*chat.ChatManager, bool) {
	return a.chatManagers.Get(deviceID)
}

// CloseChatManager 关闭指定设备的ChatManager
func (a *App) CloseChatManager(deviceID string) bool {
	if manager, exists := a.chatManagers.Get(deviceID); exists {
		manager.Close()
		a.chatManagers.Remove(deviceID)
		log.Infof("设备 %s 的ChatManager已关闭并移除", deviceID)
		return true
	}
	return false
}

// GetAllChatManagers 获取所有ChatManager的副本
func (a *App) GetAllChatManagers() map[string]*chat.ChatManager {
	// 返回副本以避免并发访问问题
	managers := make(map[string]*chat.ChatManager)
	for tuple := range a.chatManagers.IterBuffered() {
		managers[tuple.Key] = tuple.Val
	}
	return managers
}

// GetChatManagerCount 获取当前活跃的ChatManager数量
func (a *App) GetChatManagerCount() int {
	return a.chatManagers.Count()
}

// CloseAllChatManagers 关闭所有ChatManager
func (a *App) CloseAllChatManagers() {
	for tuple := range a.chatManagers.IterBuffered() {
		tuple.Val.Close()
		log.Infof("设备 %s 的ChatManager已关闭", tuple.Key)
	}

	// 清空映射
	a.chatManagers.Clear()
	log.Info("所有ChatManager已关闭")
}

// registerChatMCPTools 注册聊天相关的本地MCP工具
func (s *App) registerChatMCPTools() {
	// 调用chat包的注册函数
	chat.RegisterChatMCPTools()

	log.Info("聊天相关的本地MCP工具注册完成")
}

func (s *App) DeviceOnline(deviceID string) {
	eventData := map[string]interface{}{
		"device_id": deviceID,
	}
	providerType := viper.GetString("config_provider.type")
	provider, err := user_config.GetProvider(providerType)
	if err != nil {
		log.Errorf("GetProvider err: %+v", err)
		return
	}
	provider.NotifyDeviceEvent(context.Background(), config_types.EventDeviceOnline, eventData)
}

func (s *App) DeviceOffline(deviceID string) {
	eventData := map[string]interface{}{
		"device_id": deviceID,
	}
	providerType := viper.GetString("config_provider.type")
	provider, err := user_config.GetProvider(providerType)
	if err != nil {
		log.Errorf("GetProvider err: %+v", err)
		return
	}
	provider.NotifyDeviceEvent(context.Background(), config_types.EventDeviceOffline, eventData)
}

func (a *App) registerHandler() {
	providerType := viper.GetString("config_provider.type")
	log.Infof("registerHandler: config_provider.type=%s", providerType)
	provider, err := user_config.GetProvider(providerType)
	if err != nil {
		log.Errorf("GetProvider err: %+v", err)
		return
	}
	provider.RegisterMessageEventHandler(context.Background(), config_types.EventHandleMessageInject, a.HandleInjectMsg)
	log.Infof("registerHandler: registered paths=[%s]", config_types.EventHandleMessageInject)
}

// 向客户端注入消息
func (a *App) HandleInjectMsg(ctx context.Context, eventType string, eventData map[string]interface{}) (string, error) {
	type InjectMsg struct {
		Mode        string                    `json:"mode"`
		SkipLlm     bool                      `json:"skip_llm"`
		AutoListen  *bool                     `json:"auto_listen"`
		DeviceId    string                    `json:"device_id"`
		Message     string                    `json:"message"`
		Voice       string                    `json:"voice,omitempty"`
		TTSConfigID string                    `json:"tts_config_id,omitempty"`
		Speed       float64                   `json:"speed,omitempty"`
		AudioURL    string                    `json:"audio_url,omitempty"`
		Subtitles   []msg_types.NotifySubtitle `json:"subtitles,omitempty"`
	}
	bodyBytes, _ := json.Marshal(eventData)
	var msg InjectMsg
	err := json.Unmarshal(bodyBytes, &msg)
	if err != nil {
		log.Errorf("HandleInjectMsg error: %+v", err)
		return "", fmt.Errorf("HandleInjectMsg error")
	}

	// 验证必要参数
	if msg.DeviceId == "" {
		log.Errorf("HandleInjectMsg: device_id is required")
		return "", fmt.Errorf("device_id is required")
	}
	if msg.Message == "" && msg.AudioURL == "" {
		log.Errorf("HandleInjectMsg: message or audio_url is required")
		return "", fmt.Errorf("message or audio_url is required")
	}

	// 获取指定设备的ChatManager
	chatManager, exists := a.GetChatManager(msg.DeviceId)
	if !exists {
		log.Errorf("HandleInjectMsg: device %s not found or offline", msg.DeviceId)
		return "", fmt.Errorf("device %s not found or offline", msg.DeviceId)
	}

	// 判定是否为单向异步语音通知模式 (PR #2191)
	if strings.ToLower(strings.TrimSpace(msg.Mode)) == "notify" {
		log.Infof("HandleInjectMsg: processing notify mode for device %s", msg.DeviceId)
		if a.notifyService == nil {
			return "", fmt.Errorf("notify service not initialized")
		}

		prepareResult, err := a.notifyService.PrepareNotify(ctx, notify.PrepareNotifyRequest{
			DeviceID:    msg.DeviceId,
			Text:        msg.Message,
			Voice:       msg.Voice,
			TTSConfigID: msg.TTSConfigID,
			Speed:       msg.Speed,
			AudioURL:    msg.AudioURL,
			Subtitles:   msg.Subtitles,
		})
		if err != nil {
			log.Errorf("HandleInjectMsg: prepare notify failed: %v", err)
			return "", fmt.Errorf("prepare notify failed: %w", err)
		}

		if err := chatManager.SendNotify(prepareResult.AudioURL, prepareResult.Subtitles); err != nil {
			log.Errorf("HandleInjectMsg: send notify to device %s failed: %v", msg.DeviceId, err)
			return "", fmt.Errorf("send notify failed: %w", err)
		}

		log.Infof("HandleInjectMsg: successfully sent notify to device %s audio_url=%s subtitles_count=%d",
			msg.DeviceId, prepareResult.AudioURL, len(prepareResult.Subtitles))
		return "notify message sent successfully", nil
	}

	autoListen := true
	if msg.AutoListen != nil {
		autoListen = *msg.AutoListen
	}

	log.Debugf("HandleInjectMsg: injecting message to device %s, skip_llm: %v, auto_listen: %v, message: %s",
		msg.DeviceId, msg.SkipLlm, autoListen, msg.Message)

	// 使用ChatManager的公开方法注入消息 (speak/会话模式)
	err = chatManager.InjectMessage(msg.Message, msg.SkipLlm, autoListen)
	if err != nil {
		log.Errorf("HandleInjectMsg: failed to inject message to device %s: %v", msg.DeviceId, err)
		return "", fmt.Errorf("failed to inject message: %v", err)
	}

	return "message injected successfully", nil
}
