package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	i_redis "xiaozhi-esp32-server-golang/internal/db/redis"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	"github.com/redis/go-redis/v9"
)

var (
	memoryInstance *Memory
	once           sync.Once
)

// Memory 表示对话记忆体
type Memory struct {
	redisClient *redis.Client
	keyPrefix   string
	sync.RWMutex
}

// Init 初始化记忆体实例
func Init() error {
	var initErr error
	once.Do(func() {
		redisInstance := i_redis.GetClient()

		memoryInstance = &Memory{
			redisClient: redisInstance,
			keyPrefix:   viper.GetString("redis.key_prefix"),
		}
	})
	return initErr
}

// Get 获取记忆体实例
func Get() *Memory {
	if memoryInstance == nil {
		Init()
	}
	return memoryInstance
}

// NewMemory 创建新的记忆体实例（仅用于测试）
func NewMemory(redisClient *redis.Client) *Memory {
	return &Memory{
		redisClient: redisClient,
	}
}

// getMemoryKey 生成设备对应的 Redis key
func (m *Memory) getMemoryKey(deviceID string) string {
	return fmt.Sprintf("%s:llm:%s", m.keyPrefix, deviceID)
}

// getSystemPromptKey 生成设备对应的系统 prompt 的 Redis key
func (m *Memory) getSystemPromptKey(deviceID string) string {
	return fmt.Sprintf("%s:llm:system:%s", m.keyPrefix, deviceID)
}

// AddMessage 添加一条新的对话消息到记忆体
func (m *Memory) AddMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return nil
	}

	msg := schema.Message{
		Role:    role,
		Content: content,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed: %w", err)
	}

	key := m.getMemoryKey(deviceID)
	// 使用纳秒时间戳作为分数
	// ZREVRANGE 会返回分数从大到小的结果
	score := float64(time.Now().UnixNano())

	log.Debugf("添加消息到记忆体: %s, %s", key, string(msgBytes))

	return m.redisClient.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: string(msgBytes),
	}).Err()
}

// GetMessages 获取设备的所有对话记忆
func (m *Memory) GetMessages(ctx context.Context, deviceID string, count int) ([]schema.Message, error) {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return []schema.Message{}, nil
	}

	key := m.getMemoryKey(deviceID)

	if count == 0 {
		count = 10
	}

	// 使用 ZREVRANGE 获取最新的 N 条消息
	// 分数（时间戳）大的在前，所以需要反转顺序以保证旧消息在前
	results, err := m.redisClient.ZRevRange(ctx, key, 0, int64(count-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("get messages failed: %w", err)
	}

	// 预分配切片
	messages := make([]schema.Message, len(results))

	// 反向遍历，使旧消息在前，新消息在后
	for i := 0; i < len(results); i++ {
		if err := json.Unmarshal([]byte(results[len(results)-1-i]), &messages[i]); err != nil {
			return nil, fmt.Errorf("unmarshal message failed: %w", err)
		}
	}

	return messages, nil
}

// GetMessagesForLLM 获取适用于 LLM 的消息格式
func (m *Memory) GetMessagesForLLM(ctx context.Context, deviceID string, count int) ([]schema.Message, error) {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return []schema.Message{}, nil
	}

	// 首先获取系统 prompt
	sysPrompt, err := m.GetSystemPrompt(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("get system prompt failed: %w", err)
	}

	// 获取历史消息（已经是按时间顺序：旧->新）
	memoryMessages, err := m.GetMessages(ctx, deviceID, count)
	if err != nil {
		return nil, err
	}

	// 预分配足够的容量
	messages := make([]schema.Message, 0, len(memoryMessages)+1)

	// 系统 prompt 始终在最前面
	if sysPrompt.Content != "" {
		messages = append(messages, sysPrompt)
	}

	// 添加历史消息（已经是按时间顺序：旧->新）
	for _, msg := range memoryMessages {
		messages = append(messages, msg)
	}

	return messages, nil
}

// SetSystemPrompt 设置或更新设备的系统 prompt
func (m *Memory) SetSystemPrompt(ctx context.Context, deviceID string, prompt string) error {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return nil
	}

	key := m.getSystemPromptKey(deviceID)
	return m.redisClient.Set(ctx, key, prompt, 0).Err()
}

// GetSystemPrompt 获取设备的系统 prompt
func (m *Memory) GetSystemPrompt(ctx context.Context, deviceID string) (schema.Message, error) {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return schema.Message{}, nil
	}

	exist, _ := m.redisClient.Exists(ctx, key).Result()
	if exist <= 0 {
		if prompt := viper.GetString("system_prompt"); prompt != "" {
			m.SetSystemPrompt(ctx, deviceID, prompt)
			return schema.Message{
				Role:    schema.System,
				Content: viper.GetString("system_prompt"),
			}, nil
		}
	}

	key := m.getSystemPromptKey(deviceID)

	result, err := m.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return schema.Message{}, nil // 返回空消息结构
	}
	if err != nil {
		return schema.Message{}, fmt.Errorf("get system prompt failed: %w", err)
	}

	return schema.Message{
		Role:    schema.System,
		Content: result,
	}, nil
}

// ResetMemory 重置设备的对话记忆（包括系统 prompt）
func (m *Memory) ResetMemory(ctx context.Context, deviceID string) error {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return nil
	}

	// 删除对话历史
	historyKey := m.getMemoryKey(deviceID)
	if err := m.redisClient.Del(ctx, historyKey).Err(); err != nil {
		return fmt.Errorf("delete history failed: %w", err)
	}

	// 删除系统 prompt
	promptKey := m.getSystemPromptKey(deviceID)
	if err := m.redisClient.Del(ctx, promptKey).Err(); err != nil {
		return fmt.Errorf("delete system prompt failed: %w", err)
	}

	return nil
}

// GetLastNMessages 获取最近的 N 条消息
func (m *Memory) GetLastNMessages(ctx context.Context, deviceID string, n int64) ([]schema.Message, error) {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return []schema.Message{}, nil
	}

	key := m.getMemoryKey(deviceID)

	// 获取最后 N 条消息
	results, err := m.redisClient.ZRevRange(ctx, key, 0, n-1).Result()
	if err != nil {
		return nil, fmt.Errorf("get last messages failed: %w", err)
	}

	messages := make([]schema.Message, 0, len(results))
	for i := len(results) - 1; i >= 0; i-- { // 反转顺序以保持时间顺序
		var msg schema.Message
		if err := json.Unmarshal([]byte(results[i]), &msg); err != nil {
			return nil, fmt.Errorf("unmarshal message failed: %w", err)
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// RemoveOldMessages 删除指定时间之前的消息
func (m *Memory) RemoveOldMessages(ctx context.Context, deviceID string, before time.Time) error {
	if m.redisClient == nil {
		log.Log().Warn("redis client is nil")
		return nil
	}

	key := m.getMemoryKey(deviceID)
	score := float64(before.UnixNano())

	return m.redisClient.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", score)).Err()
}

// Summary 获取对话的摘要
func (m *Memory) GetSummary(ctx context.Context, deviceID string) (string, error) {
	return "", nil
}

// SetSummary 设置对话的摘要
func (m *Memory) SetSummary(ctx context.Context, deviceID string, summary string) error {
	return nil
}

// 进行总结
func (m *Memory) Summary(ctx context.Context, deviceID string, msgList []schema.Message) (string, error) {
	return "", nil
}
