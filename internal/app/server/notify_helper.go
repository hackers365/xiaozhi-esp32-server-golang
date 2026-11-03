package server

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"

	"xiaozhi-esp32-server-golang/internal/app/server/notify"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/pool"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

var (
	cachedLANIPOnce sync.Once
	cachedLANIP     string
)

func resolveNotifySecretKey() []byte {
	candidates := []string{
		viper.GetString("notify.secret_key"),
		viper.GetString("config_provider.manager.auth_token"),
		viper.GetString("manager.auth_token"),
		viper.GetString("internal_auth_token"),
	}
	for _, c := range candidates {
		if trimmed := strings.TrimSpace(c); trimmed != "" {
			return []byte(trimmed)
		}
	}
	return nil
}

// resolveNotifyPublicBaseURL 按优先级推导供 ESP32 设备拉取通知音频流的 HTTP(S) 基础地址：
// 1. notify.public_url
// 2. server.public_url
// 3. ota.external.websocket.url（自动将 ws/wss 转换为 http/https 并保留反向代理路径前缀）
// 4. ota.test.websocket.url
// 5. 本机非回环局域网 IPv4 + websocket.port
// 6. 兜底 http://127.0.0.1:<websocket.port>
func resolveNotifyPublicBaseURL() string {
	if u := normalizeHTTPBaseURL(viper.GetString("notify.public_url")); u != "" {
		return u
	}
	if u := normalizeHTTPBaseURL(viper.GetString("server.public_url")); u != "" {
		return u
	}
	if u := deriveBaseURLFromWebsocketURL(viper.GetString("ota.external.websocket.url")); u != "" {
		return u
	}
	if u := deriveBaseURLFromWebsocketURL(viper.GetString("ota.test.websocket.url")); u != "" {
		return u
	}

	port := viper.GetInt("websocket.port")
	if port <= 0 {
		port = 8989
	}
	if lanIP := getCachedLocalLANIPv4(); lanIP != "" {
		return fmt.Sprintf("http://%s:%d", lanIP, port)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func normalizeHTTPBaseURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return fmt.Sprintf("%s://%s%s", strings.ToLower(parsed.Scheme), parsed.Host, strings.TrimRight(parsed.Path, "/"))
	case "ws", "wss":
		return deriveBaseURLFromWebsocketURL(trimmed)
	default:
		return ""
	}
}

// deriveBaseURLFromWebsocketURL 从 OTA WebSocket 地址（例如 wss://host:55555/go_ws/xiaozhi/v1/）
// 推导出对应的 HTTP(S) 基础地址（例如 https://host:55555/go_ws）。
func deriveBaseURLFromWebsocketURL(rawWSURL string) string {
	trimmed := strings.TrimSpace(rawWSURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return ""
	}

	hostOnly := strings.ToLower(parsed.Hostname())
	if hostOnly == "" || hostOnly == "www.youdomain.cn" || hostOnly == "yourdomain.com" {
		return ""
	}

	var scheme string
	switch strings.ToLower(parsed.Scheme) {
	case "ws", "http":
		scheme = "http"
	case "wss", "https":
		scheme = "https"
	default:
		return ""
	}

	cleanPath := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/xiaozhi/mqtt_udp/v1", "/xiaozhi/v1"} {
		if strings.HasSuffix(cleanPath, suffix) {
			cleanPath = strings.TrimSuffix(cleanPath, suffix)
			break
		}
	}

	return fmt.Sprintf("%s://%s%s", scheme, parsed.Host, cleanPath)
}

func getCachedLocalLANIPv4() string {
	cachedLANIPOnce.Do(func() {
		cachedLANIP = detectLocalLANIPv4()
	})
	return cachedLANIP
}

func detectLocalLANIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var fallbackGlobal string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() {
				continue
			}
			if ip4.IsPrivate() {
				return ip4.String()
			}
			if fallbackGlobal == "" {
				fallbackGlobal = ip4.String()
			}
		}
	}
	return fallbackGlobal
}

func (app *App) resolveNotifyTTS(ctx context.Context, config *notify.NotifyConfig) (notify.TTSStreamProvider, func(), error) {
	if config == nil {
		return nil, nil, fmt.Errorf("notify config is nil")
	}

	ttsProvider, ttsConfig, err := app.resolveNotifyTTSConfig(ctx, config)
	if err != nil {
		return nil, nil, err
	}

	// 与 chat/tts.go getTTSProviderInstance 保持一致的 providerLabel 计算逻辑，共享同一资源池
	voiceID := extractNotifyVoiceID(ttsProvider, ttsConfig)
	providerLabel := ttsProvider
	if voiceID != "" {
		providerLabel = fmt.Sprintf("%s:%s", ttsProvider, voiceID)
	}

	ttsWrapper, err := pool.Acquire[tts.TTSProvider]("tts", providerLabel, ttsConfig)
	if err == nil && ttsWrapper != nil {
		return ttsWrapper.GetProvider(), func() { pool.Release(ttsWrapper) }, nil
	}

	log.Warnf("NotifyService: acquire tts from pool failed for %s (%v), fallback to direct provider", providerLabel, err)
	p, err := tts.GetTTSProvider(ttsProvider, ttsConfig)
	if err != nil {
		return nil, nil, err
	}
	return p, func() { _ = p.Close() }, nil
}

func (app *App) resolveNotifyTTSConfig(ctx context.Context, config *notify.NotifyConfig) (string, map[string]interface{}, error) {
	var ttsProvider string
	ttsConfig := make(map[string]interface{})

	targetConfigID := strings.TrimSpace(config.TTSConfigID)
	if targetConfigID != "" {
		provider, cfg, ok := lookupTTSConfigByID(viper.Get("tts"), targetConfigID)
		if !ok {
			return "", nil, fmt.Errorf("tts config %q not found", targetConfigID)
		}
		ttsProvider = provider
		ttsConfig = cfg
	} else {
		// 1. 优先从本地活跃会话的 DeviceConfig 读取
		if app != nil && strings.TrimSpace(config.DeviceID) != "" {
			if chatManager, exists := app.GetChatManager(config.DeviceID); exists && chatManager != nil && chatManager.GetClientState() != nil {
				deviceCfg := chatManager.GetClientState().DeviceConfig
				ttsProvider = strings.TrimSpace(deviceCfg.Tts.Provider)
				ttsConfig = cloneConfigMap(deviceCfg.Tts.Config)
			}
		}

		// 2. 若本地无活跃 ChatManager（如跨节点负载均衡或会话已回收），向配置中心拉取该设备的用户配置
		if ttsProvider == "" && strings.TrimSpace(config.DeviceID) != "" {
			if providerType := strings.TrimSpace(viper.GetString("config_provider.type")); providerType != "" {
				if cfgProvider, err := user_config.GetProvider(providerType); err == nil && cfgProvider != nil {
					if ucfg, err := cfgProvider.GetUserConfig(ctx, config.DeviceID); err == nil && strings.TrimSpace(ucfg.Tts.Provider) != "" {
						ttsProvider = strings.TrimSpace(ucfg.Tts.Provider)
						ttsConfig = cloneConfigMap(ucfg.Tts.Config)
					}
				}
			}
		}

		// 3. 兜底系统全局默认 TTS 配置
		if ttsProvider == "" {
			ttsProvider, ttsConfig = lookupDefaultTTSFromViper(viper.Get("tts"))
		} else if len(ttsConfig) == 0 {
			if sub := viper.GetStringMap("tts." + ttsProvider); len(sub) > 0 {
				ttsConfig = cloneConfigMap(sub)
			}
		}
	}

	if voice := strings.TrimSpace(config.Voice); voice != "" {
		if ttsProvider == "cosyvoice" {
			ttsConfig["spk_id"] = voice
		} else {
			ttsConfig["voice"] = voice
		}
	}
	if config.Speed > 0 {
		ttsConfig["speed"] = config.Speed
	}

	return ttsProvider, ttsConfig, nil
}

func lookupTTSConfigByID(ttsRaw interface{}, targetConfigID string) (string, map[string]interface{}, bool) {
	ttsMap, ok := ttsRaw.(map[string]interface{})
	if !ok || len(ttsMap) == 0 {
		return "", nil, false
	}

	itemRaw, found := ttsMap[targetConfigID]
	if !found {
		for k, v := range ttsMap {
			if strings.EqualFold(k, targetConfigID) {
				itemRaw = v
				found = true
				break
			}
		}
	}
	if !found {
		return "", nil, false
	}

	itemMap, ok := itemRaw.(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	provider, _ := itemMap["provider"].(string)
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "", nil, false
	}

	cfg := make(map[string]interface{}, len(itemMap))
	for k, v := range itemMap {
		switch k {
		case "name", "provider", "is_default", "config_id":
			continue
		default:
			cfg[k] = v
		}
	}
	return provider, cfg, true
}

func lookupDefaultTTSFromViper(ttsRaw interface{}) (string, map[string]interface{}) {
	if explicitProvider := strings.TrimSpace(viper.GetString("tts.provider")); explicitProvider != "" {
		return explicitProvider, cloneConfigMap(viper.GetStringMap("tts." + explicitProvider))
	}

	if ttsMap, ok := ttsRaw.(map[string]interface{}); ok && len(ttsMap) > 0 {
		keys := make([]string, 0, len(ttsMap))
		for k := range ttsMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var firstProvider string
		var firstCfg map[string]interface{}
		for _, k := range keys {
			provider, cfg, ok := lookupTTSConfigByID(ttsMap, k)
			if !ok {
				continue
			}
			itemMap, _ := ttsMap[k].(map[string]interface{})
			if isDefault, _ := itemMap["is_default"].(bool); isDefault {
				return provider, cfg
			}
			if firstProvider == "" {
				firstProvider = provider
				firstCfg = cfg
			}
		}
		if firstProvider != "" {
			return firstProvider, firstCfg
		}
	}

	return "edge", make(map[string]interface{})
}

func extractNotifyVoiceID(ttsProvider string, config map[string]interface{}) string {
	if config == nil {
		return ""
	}
	provider := strings.TrimSpace(ttsProvider)
	if provider == "" {
		provider, _ = config["provider"].(string)
	}
	if provider == "cosyvoice" {
		if spkID, ok := config["spk_id"].(string); ok {
			return strings.TrimSpace(spkID)
		}
		return ""
	}
	if voice, ok := config["voice"].(string); ok {
		return strings.TrimSpace(voice)
	}
	return ""
}

func cloneConfigMap(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
