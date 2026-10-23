package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	log "xiaozhi-esp32-server-golang/logger"
)

const (
	RedisNotifyKeyPrefix = "xiaozhi:notify:"
	DefaultNotifyTTL     = 15 * time.Minute
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
	secretKey      []byte
	publicBaseURL  string
	redisClient    *redis.Client
	ttsResolver    TTSResolver
	memoryFallback sync.Map
}

func NewNotifyService(secretKey []byte, publicBaseURL string, redisClient *redis.Client, ttsResolver TTSResolver) *NotifyService {
	if len(secretKey) == 0 {
		secretKey = []byte("xiaozhi_notify_default_secret_key_32bytes!")
	}
	s := &NotifyService{
		secretKey:     secretKey,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		redisClient:   redisClient,
		ttsResolver:   ttsResolver,
	}

	// 定时清理内存降级缓存
	go s.cleanupMemoryFallback()
	return s
}

func (s *NotifyService) SetPublicBaseURL(baseURL string) {
	s.publicBaseURL = strings.TrimRight(baseURL, "/")
}

func (s *NotifyService) SetRedisClient(client *redis.Client) {
	s.redisClient = client
}

func (s *NotifyService) SetTTSResolver(resolver TTSResolver) {
	s.ttsResolver = resolver
}

func (s *NotifyService) cleanupMemoryFallback() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
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

// PrepareNotify 准备通知数据：生成 UUID，存入 Redis，生成加密 Token 与 audio_url，生成字幕
func (s *NotifyService) PrepareNotify(ctx context.Context, req PrepareNotifyRequest) (*PrepareNotifyResult, error) {
	// 如果调用方已提供了现成的外部 AudioURL，直接透传
	if strings.TrimSpace(req.AudioURL) != "" {
		subtitles := req.Subtitles
		if len(subtitles) == 0 && strings.TrimSpace(req.Text) != "" {
			subtitles = EstimateSubtitles(req.Text, req.Speed)
		}
		return &PrepareNotifyResult{
			AudioURL:  req.AudioURL,
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
	if s.redisClient != nil {
		redisKey := RedisNotifyKeyPrefix + u
		if err := s.redisClient.Set(ctx, redisKey, cfgBytes, DefaultNotifyTTL).Err(); err != nil {
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
	baseURL := s.publicBaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8990"
	}
	audioURL := fmt.Sprintf("%s/xiaozhi/notify/stream?token=%s", baseURL, tokenStr)

	return &PrepareNotifyResult{
		UUID:      u,
		AudioURL:  audioURL,
		Subtitles: subtitles,
	}, nil
}

// HandleStream HTTP 端点处理器：接收 ESP32 的 GET 请求，解密 Token，从 Redis 查配置，实时流式返回 Ogg Opus
func (s *NotifyService) HandleStream(w http.ResponseWriter, r *http.Request) {
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

	// 3. 获取 TTS 提供者
	if s.ttsResolver == nil {
		log.Errorf("NotifyService: ttsResolver is nil")
		http.Error(w, "tts service not initialized", http.StatusInternalServerError)
		return
	}

	ttsProvider, releaseFunc, err := s.ttsResolver(r.Context(), cfg)
	if err != nil || ttsProvider == nil {
		log.Errorf("NotifyService: resolve tts provider failed: %v", err)
		http.Error(w, "resolve tts provider failed", http.StatusInternalServerError)
		return
	}
	if releaseFunc != nil {
		defer releaseFunc()
	}

	// 4. 设置 HTTP 响应头
	w.Header().Set("Content-Type", "audio/ogg")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
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

	// 5. 启动流式 TTS 合成
	opusChan, err := ttsProvider.TextToSpeechStream(r.Context(), cfg.Text, sampleRate, 1, frameDuration)
	if err != nil {
		log.Errorf("NotifyService: start TextToSpeechStream failed: %v", err)
		return
	}

	// 6. 实时封装 Ogg Opus 并写入 HTTP 连接
	if err := StreamOpusToOgg(r.Context(), w, flusher, opusChan, sampleRate, 1, frameDuration); err != nil {
		log.Warnf("NotifyService: stream opus to ogg interrupted: %v", err)
	} else {
		log.Infof("NotifyService: finish streaming TTS for device=%s uuid=%s", cfg.DeviceID, cfg.UUID)
	}
}

func (s *NotifyService) getNotifyConfig(ctx context.Context, u string) (*NotifyConfig, error) {
	if s.redisClient != nil {
		redisKey := RedisNotifyKeyPrefix + u
		data, err := s.redisClient.Get(ctx, redisKey).Bytes()
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
