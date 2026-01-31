package pool

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/asr"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/domain/vad"
	vad_inter "xiaozhi-esp32-server-golang/internal/domain/vad/inter"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/spf13/viper"
)

var (
	globalManager *UniversalResourcePoolManager
	once          sync.Once
)

// UniversalResourcePoolManager 通用资源池管理器
type UniversalResourcePoolManager struct {
	pools        map[string]*util.ResourcePool // key格式: "resourceType:provider"
	creators     map[string]interface{}        // 已注册的创建函数
	closeFuncs   map[string]func(interface{}) error
	isValidFuncs map[string]func(interface{}) bool
	resetFuncs   map[string]func(interface{}) error
	mu           sync.RWMutex
}

// GetGlobalResourcePoolManager 获取全局资源池管理器（单例）
func GetGlobalResourcePoolManager() *UniversalResourcePoolManager {
	once.Do(func() {
		globalManager = &UniversalResourcePoolManager{
			pools:        make(map[string]*util.ResourcePool),
			creators:     make(map[string]interface{}),
			closeFuncs:   make(map[string]func(interface{}) error),
			isValidFuncs: make(map[string]func(interface{}) bool),
			resetFuncs:   make(map[string]func(interface{}) error),
		}
		log.Info("通用资源池管理器已初始化")
	})
	return globalManager
}

// ResourceTypeOption 资源类型注册选项
type ResourceTypeOption func(*ResourceTypeConfig)

// ResourceTypeConfig 资源类型配置
type ResourceTypeConfig struct {
	CloseFunc   func(interface{}) error
	IsValidFunc func(interface{}) bool
	ResetFunc   func(interface{}) error
}

// WithCloseFunc 设置关闭函数
func WithCloseFunc(fn func(interface{}) error) ResourceTypeOption {
	return func(c *ResourceTypeConfig) {
		c.CloseFunc = fn
	}
}

// WithIsValidFunc 设置验证函数
func WithIsValidFunc(fn func(interface{}) bool) ResourceTypeOption {
	return func(c *ResourceTypeConfig) {
		c.IsValidFunc = fn
	}
}

// WithResetFunc 设置重置函数
func WithResetFunc(fn func(interface{}) error) ResourceTypeOption {
	return func(c *ResourceTypeConfig) {
		c.ResetFunc = fn
	}
}

// RegisterResourceType 注册资源类型（外部调用）
// resourceType: 资源类型名称（如 "vad", "asr", "custom_type" 等）
// creator: 资源创建函数
// opts: 可选配置（closeFunc, isValidFunc, resetFunc）
func RegisterResourceType[T any](
	resourceType string,
	creator CreatorFunc[T],
	opts ...ResourceTypeOption,
) error {
	mgr := GetGlobalResourcePoolManager()
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// 检查是否已注册
	if _, exists := mgr.creators[resourceType]; exists {
		return fmt.Errorf("资源类型 %s 已注册", resourceType)
	}

	// 注册 creator
	mgr.creators[resourceType] = creator

	// 应用选项
	config := &ResourceTypeConfig{}
	for _, opt := range opts {
		opt(config)
	}

	if config.CloseFunc != nil {
		mgr.closeFuncs[resourceType] = config.CloseFunc
	}
	if config.IsValidFunc != nil {
		mgr.isValidFuncs[resourceType] = config.IsValidFunc
	}
	if config.ResetFunc != nil {
		mgr.resetFuncs[resourceType] = config.ResetFunc
	}

	log.Infof("注册资源类型: %s", resourceType)
	return nil
}

// GenerateConfigKey 生成配置键（用于区分不同配置的资源池）
func GenerateConfigKey(provider string, config map[string]interface{}) string {
	configJSON, err := json.Marshal(config)
	if err != nil {
		log.Warnf("序列化配置失败，使用provider作为key: %v", err)
		return provider
	}
	hash := md5.Sum([]byte(provider + string(configJSON)))
	return hex.EncodeToString(hash[:])
}

// getOrCreatePool 获取或创建资源池（泛型版本）
func getOrCreatePool[T any](
	resourceType, provider string,
	config map[string]interface{},
) (*util.ResourcePool, error) {
	mgr := GetGlobalResourcePoolManager()
	// 所有资源池的 key 格式统一为：类型:provider
	poolKey := fmt.Sprintf("%s:%s", resourceType, provider)
	configKey := provider // configKey 也使用 provider，用于 ResourceWrapper

	mgr.mu.RLock()
	pool, exists := mgr.pools[poolKey]
	mgr.mu.RUnlock()

	if exists {
		return pool, nil
	}

	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// 双重检查
	if pool, exists := mgr.pools[poolKey]; exists {
		return pool, nil
	}

	// 获取已注册的 creator
	creatorInterface, exists := mgr.creators[resourceType]
	if !exists {
		return nil, fmt.Errorf("资源类型未注册: %s (请先调用 RegisterResourceType 注册)", resourceType)
	}

	// 类型断言获取泛型 creator
	creator, ok := creatorInterface.(CreatorFunc[T])
	if !ok {
		return nil, fmt.Errorf("资源类型 %s 的 creator 类型不匹配", resourceType)
	}

	// 创建泛型资源工厂
	factory := &ResourceFactory[T]{
		resourceType: resourceType,
		provider:     provider,
		config:       config,
		configKey:    configKey,
		creator:      creator,
		closeFunc: func(p T) error {
			if closeFunc := mgr.closeFuncs[resourceType]; closeFunc != nil {
				return closeFunc(any(p))
			}
			return nil
		},
		isValidFunc: func(p T) bool {
			if isValidFunc := mgr.isValidFuncs[resourceType]; isValidFunc != nil {
				return isValidFunc(any(p))
			}
			return true
		},
		resetFunc: func(p T) error {
			if resetFunc := mgr.resetFuncs[resourceType]; resetFunc != nil {
				return resetFunc(any(p))
			}
			return nil
		},
	}

	// 获取资源池配置（所有资源类型共享默认配置）
	poolConfig := getPoolConfig()

	// 创建资源池
	pool, err := util.NewResourcePool(poolConfig, factory)
	if err != nil {
		return nil, fmt.Errorf("创建资源池失败 [%s:%s]: %w", resourceType, configKey, err)
	}

	mgr.pools[poolKey] = pool
	log.Infof("创建资源池: type=%s, provider=%s, configKey=%s", resourceType, provider, configKey)

	return pool, nil
}

// Acquire 获取资源（泛型版本，类型安全，支持懒加载）
// T: 资源类型
// resourceType: 资源类型字符串（vad/asr/llm/tts等）
// provider: 提供者名称
// config: 配置信息
func Acquire[T any](
	resourceType, provider string,
	config map[string]interface{},
) (*ResourceWrapper[T], error) {
	pool, err := getOrCreatePool[T](resourceType, provider, config)
	if err != nil {
		return nil, err
	}

	resource, err := pool.Acquire()
	if err != nil {
		return nil, fmt.Errorf("获取资源失败 [%s:%s]: %w", resourceType, provider, err)
	}

	wrapper, ok := resource.(*ResourceWrapper[T])
	if !ok {
		pool.Release(resource)
		return nil, fmt.Errorf("资源类型错误: 期望 ResourceWrapper[%T]", *new(T))
	}

	return wrapper, nil
}

// Release 归还资源（泛型版本，类型安全）
func Release[T any](wrapper *ResourceWrapper[T]) error {
	if wrapper == nil {
		return nil
	}

	mgr := GetGlobalResourcePoolManager()
	// 所有资源池的 key 格式统一为：类型:provider
	poolKey := fmt.Sprintf("%s:%s", wrapper.resourceType, wrapper.configKey)

	mgr.mu.RLock()
	pool, exists := mgr.pools[poolKey]
	mgr.mu.RUnlock()

	if !exists {
		log.Warnf("资源池不存在: %s", poolKey)
		return nil
	}

	return pool.Release(wrapper)
}

// GetStats 获取所有资源池的统计信息
func GetStats() map[string]interface{} {
	mgr := GetGlobalResourcePoolManager()
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	stats := make(map[string]interface{})

	for poolKey, pool := range mgr.pools {
		stats[poolKey] = pool.Stats()
	}

	return stats
}

// StartStatsMonitor 启动资源池统计监控，每 interval 输出一次统计信息到日志
func StartStatsMonitor(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Debugf("资源池统计监控已停止")
				return
			case <-ticker.C:
				stats := GetStats()
				if len(stats) > 0 {
					statsJSON, err := json.MarshalIndent(stats, "", "  ")
					if err != nil {
						log.Errorf("序列化资源池统计信息失败: %v", err)
						continue
					}
					log.Infof("========== 全局资源池统计信息 ==========")
					log.Infof("统计时间: %s", time.Now().Format("2006-01-02 15:04:05"))
					log.Infof("资源池数量: %d", len(stats))
					log.Infof("详细信息:\n%s", string(statsJSON))
					log.Infof("========================================")
				} else {
					log.Infof("========== 全局资源池统计信息 ==========")
					log.Infof("统计时间: %s", time.Now().Format("2006-01-02 15:04:05"))
					log.Infof("当前没有活跃的资源池")
					log.Infof("========================================")
				}
			}
		}
	}()
	log.Infof("资源池统计监控已启动，每 %v 输出一次统计信息到日志", interval)
}

// Close 关闭所有资源池
func Close() error {
	mgr := GetGlobalResourcePoolManager()
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	var errs []error

	for poolKey, pool := range mgr.pools {
		if err := pool.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭资源池 %s 失败: %w", poolKey, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("关闭资源池时发生错误: %v", errs)
	}

	return nil
}

// getPoolConfig 从配置中获取资源池配置（所有资源类型共享默认配置）
func getPoolConfig() *util.PoolConfig {
	// 使用默认配置
	config := util.DefaultConfig()

	// 如果配置了 resource_pools，则覆盖默认值
	if viper.IsSet("resource_pools.max_size") {
		config.MaxSize = viper.GetInt("resource_pools.max_size")
	}
	if viper.IsSet("resource_pools.min_size") {
		config.MinSize = viper.GetInt("resource_pools.min_size")
	}
	if viper.IsSet("resource_pools.max_idle") {
		config.MaxIdle = viper.GetInt("resource_pools.max_idle")
	}
	if viper.IsSet("resource_pools.acquire_timeout") {
		config.AcquireTimeout = viper.GetDuration("resource_pools.acquire_timeout")
	}
	if viper.IsSet("resource_pools.idle_timeout") {
		config.IdleTimeout = viper.GetDuration("resource_pools.idle_timeout")
	}
	if viper.IsSet("resource_pools.validate_on_borrow") {
		config.ValidateOnBorrow = viper.GetBool("resource_pools.validate_on_borrow")
	}
	if viper.IsSet("resource_pools.validate_on_return") {
		config.ValidateOnReturn = viper.GetBool("resource_pools.validate_on_return")
	}

	return config
}

// init 初始化内置资源类型
func init() {
	// 注册 VAD 资源类型
	RegisterResourceType[vad_inter.VAD](
		"vad",
		func(rt, p string, cfg map[string]interface{}) (vad_inter.VAD, error) {
			vadProvider, err := vad.AcquireVAD(p, cfg)
			if err != nil {
				return nil, err
			}
			if vadProvider != nil {
				vadProvider.Reset()
			}
			return vadProvider, nil
		},
		WithCloseFunc(func(p interface{}) error {
			if vadProvider, ok := p.(vad_inter.VAD); ok && vadProvider != nil {
				return vadProvider.Close()
			}
			return nil
		}),
		WithIsValidFunc(func(p interface{}) bool {
			if vadProvider, ok := p.(vad_inter.VAD); ok && vadProvider != nil {
				return vadProvider.IsValid()
			}
			return false
		}),
		WithResetFunc(func(p interface{}) error {
			if vadProvider, ok := p.(vad_inter.VAD); ok && vadProvider != nil {
				return vadProvider.Reset()
			}
			return nil
		}),
	)

	// 注册 ASR 资源类型
	RegisterResourceType[asr.AsrProvider](
		"asr",
		func(rt, p string, cfg map[string]interface{}) (asr.AsrProvider, error) {
			return asr.NewAsrProvider(p, cfg)
		},
		WithIsValidFunc(func(p interface{}) bool {
			if asrProvider, ok := p.(asr.AsrProvider); ok && asrProvider != nil {
				return asrProvider.IsValid()
			}
			return false
		}),
		WithCloseFunc(func(p interface{}) error {
			if asrProvider, ok := p.(asr.AsrProvider); ok && asrProvider != nil {
				return asrProvider.Close()
			}
			return nil
		}),
	)

	// 注册 LLM 资源类型
	RegisterResourceType[llm.LLMProvider](
		"llm",
		func(rt, p string, cfg map[string]interface{}) (llm.LLMProvider, error) {
			llmType, ok := cfg["type"].(string)
			if !ok {
				llmType = p
			}
			return llm.GetLLMProvider(llmType, cfg)
		},
		WithIsValidFunc(func(p interface{}) bool {
			if llmProvider, ok := p.(llm.LLMProvider); ok && llmProvider != nil {
				return llmProvider.IsValid()
			}
			return false
		}),
		WithCloseFunc(func(p interface{}) error {
			if llmProvider, ok := p.(llm.LLMProvider); ok && llmProvider != nil {
				return llmProvider.Close()
			}
			return nil
		}),
	)

	// 注册 TTS 资源类型
	// 资源池 key 可能为 "provider" 或 "provider:voiceID"，GetTTSProvider 只识别 provider 名，需从 key 中解析
	RegisterResourceType[tts.TTSProvider](
		"tts",
		func(rt, p string, cfg map[string]interface{}) (tts.TTSProvider, error) {
			providerName := p
			if idx := strings.Index(p, ":"); idx > 0 {
				providerName = p[:idx]
			}
			return tts.GetTTSProvider(providerName, cfg)
		},
		WithIsValidFunc(func(p interface{}) bool {
			if ttsProvider, ok := p.(tts.TTSProvider); ok && ttsProvider != nil {
				return ttsProvider.IsValid()
			}
			return false
		}),
		WithCloseFunc(func(p interface{}) error {
			if ttsProvider, ok := p.(tts.TTSProvider); ok && ttsProvider != nil {
				return ttsProvider.Close()
			}
			return nil
		}),
	)
}
