package notify

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	log "xiaozhi-esp32-server-golang/logger"
)

const (
	RedisNotifyKeyPrefix     = "xiaozhi:notify:"
	DefaultNotifyTTL         = 15 * time.Minute
	defaultFirstFrameTimeout = 15 * time.Second
)

// TTSStreamProvider 流式 TTS 提供者接口
type TTSStreamProvider interface {
	TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error)
}

// TTSResolver 用于解析并获取 TTS 提供者实例及其释放回调
type TTSResolver func(ctx context.Context, config *NotifyConfig) (TTSStreamProvider, func(), error)

type memoryItem struct {
	config    *NotifyConfig
	expiresAt time.Time
}

type NotifyService struct {
	secretKey         []byte
	mu                sync.RWMutex
	publicBaseURL     string
	baseURLResolver   func() string
	redisClient       *redis.Client
	ttsResolver       TTSResolver
	firstFrameTimeout time.Duration
	memoryFallback    sync.Map
	stopCh            chan struct{}
	closeOnce         sync.Once
}

func NewNotifyService(secretKey []byte, publicBaseURL string, redisClient *redis.Client, ttsResolver TTSResolver) *NotifyService {
	var keyCopy []byte
	if len(secretKey) == 0 {
		keyCopy = make([]byte, 32)
		if _, err := rand.Read(keyCopy); err != nil {
			// 极端情况下系统熵源异常时使用随机 UUID 组合兜底
			keyCopy = []byte(uuid.NewString() + uuid.NewString())[:32]
		}
		log.Warnf("NotifyService: secretKey is empty, generated ephemeral 32-byte random key (configure notify.secret_key for multi-instance deployments)")
	} else {
		keyCopy = append([]byte(nil), secretKey...)
	}

	s := &NotifyService{
		secretKey:         keyCopy,
		publicBaseURL:     strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		redisClient:       redisClient,
		ttsResolver:       ttsResolver,
		firstFrameTimeout: defaultFirstFrameTimeout,
		stopCh:            make(chan struct{}),
	}

	// 定时清理内存降级缓存
	go s.cleanupMemoryFallback()
	return s
}

// Close 停止后台内存缓存清理协程
func (s *NotifyService) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *NotifyService) SetPublicBaseURL(baseURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publicBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func (s *NotifyService) GetPublicBaseURL() string {
	return s.resolveBaseURL()
}

// SetBaseURLResolver 设置动态解析外部可访问基础 URL 的回调（用于配置热更新场景）
func (s *NotifyService) SetBaseURLResolver(resolver func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseURLResolver = resolver
}

func (s *NotifyService) SetRedisClient(client *redis.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redisClient = client
}

func (s *NotifyService) SetTTSResolver(resolver TTSResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttsResolver = resolver
}

func (s *NotifyService) resolveBaseURL() string {
	s.mu.RLock()
	staticURL := s.publicBaseURL
	resolver := s.baseURLResolver
	s.mu.RUnlock()

	if resolver != nil {
		if resolved := strings.TrimRight(strings.TrimSpace(resolver()), "/"); resolved != "" {
			return resolved
		}
	}
	if staticURL != "" {
		return staticURL
	}
	return "http://127.0.0.1:8990"
}

func (s *NotifyService) getRedisClient() *redis.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.redisClient
}

func (s *NotifyService) getTTSResolver() TTSResolver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ttsResolver
}

func (s *NotifyService) getFirstFrameTimeout() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.firstFrameTimeout <= 0 {
		return defaultFirstFrameTimeout
	}
	return s.firstFrameTimeout
}

func (s *NotifyService) cleanupMemoryFallback() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			s.memoryFallback.Range(func(key, value interface{}) bool {
				if item, ok := value.(memoryItem); ok {
					if now.After(item.expiresAt) {
						s.memoryFallback.Delete(key)
					}
				}
				return true
			})
		}
	}
}

// PrepareNotify 准备通知数据：生成 UUID，存入 Redis，生成加密 Token 与 audio_url，生成字幕
func (s *NotifyService) PrepareNotify(ctx context.Context, req PrepareNotifyRequest) (*PrepareNotifyResult, error) {
	// 如果调用方已提供了现成的外部 AudioURL，校验合法性后透传
	if rawAudioURL := strings.TrimSpace(req.AudioURL); rawAudioURL != "" {
		parsedURL, err := url.Parse(rawAudioURL)
		if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return nil, errors.New("invalid audio_url: must be a valid http or https URL")
		}
		subtitles := req.Subtitles
		if len(subtitles) == 0 && strings.TrimSpace(req.Text) != "" {
			subtitles = EstimateSubtitles(req.Text, req.Speed)
		}
		return &PrepareNotifyResult{
			AudioURL:  rawAudioURL,
			Subtitles: subtitles,
		}, nil
	}

	trimmedText := strings.TrimSpace(req.Text)
	if trimmedText == "" {
		return nil, errors.New("notify text cannot be empty")
	}

	u := uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(DefaultNotifyTTL)
	nonce := uuid.New().String()[:8]

	subtitles := req.Subtitles
	if len(subtitles) == 0 {
		subtitles = EstimateSubtitles(trimmedText, req.Speed)
	}

	cfg := &NotifyConfig{
		UUID:          u,
		DeviceID:      req.DeviceID,
		Text:          trimmedText,
		Voice:         req.Voice,
		TTSConfigID:   req.TTSConfigID,
		Speed:         req.Speed,
		SampleRate:    16000,
		FrameDuration: 60,
		Subtitles:     subtitles,
		CreatedAt:     now.Unix(),
	}

	// 1. 存储配置到 Redis (Key: xiaozhi:notify:{uuid})
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal notify config failed: %w", err)
	}

	storedInRedis := false
	if redisClient := s.getRedisClient(); redisClient != nil {
		redisKey := RedisNotifyKeyPrefix + u
		if err := redisClient.Set(ctx, redisKey, cfgBytes, DefaultNotifyTTL).Err(); err != nil {
			log.Warnf("NotifyService: write to redis failed (%v), fallback to memory", err)
		} else {
			storedInRedis = true
		}
	}

	// 同时写入内存 fallback 缓存
	s.memoryFallback.Store(u, memoryItem{
		config:    cfg,
		expiresAt: expiresAt,
	})
	log.Debugf("NotifyService: saved notify config uuid=%s storedInRedis=%v", u, storedInRedis)

	// 2. 生成精简 Token (仅含 u, e, n)
	tokenPayload := &NotifyTokenPayload{
		UUID:      u,
		ExpiresAt: expiresAt.Unix(),
		Nonce:     nonce,
	}

	tokenStr, err := EncryptToken(tokenPayload, s.secretKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt notify token failed: %w", err)
	}

	// 3. 构造 audio_url
	baseURL := s.resolveBaseURL()
	audioURL := fmt.Sprintf("%s/xiaozhi/notify/stream?token=%s", baseURL, url.QueryEscape(tokenStr))

	return &PrepareNotifyResult{
		UUID:      u,
		AudioURL:  audioURL,
		Subtitles: subtitles,
	}, nil
}

// HandleStream HTTP 端点处理器：接收 ESP32 的 GET 请求，解密 Token，从 Redis 查配置，实时流式返回 Ogg Opus
func (s *NotifyService) HandleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	// 1. 解密并校验 Token
	tokenPayload, err := DecryptToken(tokenStr, s.secretKey)
	if err != nil {
		log.Warnf("NotifyService: decrypt token failed: %v", err)
		if errors.Is(err, ErrTokenExpired) {
			http.Error(w, "token expired", http.StatusForbidden)
		} else {
			http.Error(w, "invalid token", http.StatusForbidden)
		}
		return
	}

	// 2. 从 Redis / 内存获取配置
	cfg, err := s.getNotifyConfig(r.Context(), tokenPayload.UUID)
	if err != nil || cfg == nil {
		log.Warnf("NotifyService: notify config not found for uuid=%s: %v", tokenPayload.UUID, err)
		http.Error(w, "notify config not found or expired", http.StatusNotFound)
		return
	}

	// 3. 获取 TTS 提供者，使用派生 streamCtx 确保在归还资源池前必定先取消后台 TTS 合成流
	ttsResolver := s.getTTSResolver()
	if ttsResolver == nil {
		log.Errorf("NotifyService: ttsResolver is nil")
		http.Error(w, "tts service not initialized", http.StatusInternalServerError)
		return
	}

	streamCtx, cancelStream := context.WithCancel(r.Context())
	defer cancelStream()

	ttsProvider, releaseFunc, err := ttsResolver(streamCtx, cfg)
	if err != nil || ttsProvider == nil {
		log.Errorf("NotifyService: resolve tts provider failed: %v", err)
		http.Error(w, "resolve tts provider failed", http.StatusInternalServerError)
		return
	}
	if releaseFunc != nil {
		defer func() {
			cancelStream()
			releaseFunc()
		}()
	}

	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	frameDuration := cfg.FrameDuration
	if frameDuration <= 0 {
		frameDuration = 60
	}

	log.Infof("NotifyService: start streaming TTS for device=%s uuid=%s text=%q sampleRate=%d",
		cfg.DeviceID, cfg.UUID, cfg.Text, sampleRate)

	// 4. 启动流式 TTS 合成并等待首帧就绪后再发送 200 OK，避免空流/上游失败导致客户端收到假 200
	opusChan, err := ttsProvider.TextToSpeechStream(streamCtx, cfg.Text, sampleRate, 1, frameDuration)
	if err != nil {
		log.Errorf("NotifyService: start TextToSpeechStream failed: %v", err)
		http.Error(w, "start tts stream failed", http.StatusBadGateway)
		return
	}

	var firstPacket []byte
	firstFrameTimer := time.NewTimer(s.getFirstFrameTimeout())
	for len(firstPacket) == 0 {
		select {
		case <-streamCtx.Done():
			firstFrameTimer.Stop()
			log.Warnf("NotifyService: request cancelled before first TTS frame for device=%s uuid=%s", cfg.DeviceID, cfg.UUID)
			return
		case <-firstFrameTimer.C:
			log.Errorf("NotifyService: timeout waiting first TTS frame for device=%s uuid=%s", cfg.DeviceID, cfg.UUID)
			http.Error(w, "tts first frame timeout", http.StatusGatewayTimeout)
			return
		case pkt, ok := <-opusChan:
			if !ok {
				firstFrameTimer.Stop()
				log.Errorf("NotifyService: TTS stream closed without audio frames for device=%s uuid=%s", cfg.DeviceID, cfg.UUID)
				http.Error(w, "empty tts audio stream", http.StatusBadGateway)
				return
			}
			if len(pkt) > 0 {
				firstPacket = pkt
			}
		}
	}
	firstFrameTimer.Stop()

	// 5. 设置 HTTP 响应头（不手动设置 Transfer-Encoding/Connection 以兼容 HTTP/1.1 与 HTTP/2 反向代理）
	w.Header().Set("Content-Type", "audio/ogg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	// 6. 实时封装 Ogg Opus 并写入 HTTP 连接
	if err := streamOpusToOggWithFirstPacket(streamCtx, w, flusher, firstPacket, opusChan, sampleRate, 1, frameDuration); err != nil {
		log.Warnf("NotifyService: stream opus to ogg interrupted: %v", err)
	} else {
		log.Infof("NotifyService: finish streaming TTS for device=%s uuid=%s", cfg.DeviceID, cfg.UUID)
	}
}

func (s *NotifyService) getNotifyConfig(ctx context.Context, u string) (*NotifyConfig, error) {
	if redisClient := s.getRedisClient(); redisClient != nil {
		redisKey := RedisNotifyKeyPrefix + u
		data, err := redisClient.Get(ctx, redisKey).Bytes()
		if err == nil && len(data) > 0 {
			var cfg NotifyConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				return &cfg, nil
			}
		}
	}

	// 检查内存 fallback
	if val, ok := s.memoryFallback.Load(u); ok {
		if item, ok := val.(memoryItem); ok {
			if time.Now().Before(item.expiresAt) {
				return item.config, nil
			}
		}
	}

	return nil, errors.New("config not found")
}
