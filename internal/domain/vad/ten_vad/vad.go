package ten_vad

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	log "xiaozhi-esp32-server-golang/logger"

	"xiaozhi-esp32-server-golang/internal/domain/vad/hysteresis"
	. "xiaozhi-esp32-server-golang/internal/domain/vad/inter"
)

// VAD默认配置
var defaultVADConfig = map[string]interface{}{
	"hop_size":      512,
	"threshold":     0.3,
	"threshold_low": 0.3,
}

// TenVAD TEN-VAD模型实现
type TenVAD struct {
	handle       unsafe.Pointer
	hopSize      int
	threshold    float32
	thresholdLow float32
	detector     *hysteresis.Detector
	mu           sync.Mutex
}

// NewTenVAD 创建TenVAD实例
func NewTenVAD(config map[string]interface{}) (*TenVAD, error) {
	hopSize, ok := config["hop_size"].(int)
	if !ok {
		// 尝试从 float64 转换
		if hopSizeFloat, ok := config["hop_size"].(float64); ok {
			hopSize = int(hopSizeFloat)
		} else {
			hopSize = 512 // 默认值
		}
	}

	threshold, ok := config["threshold"].(float64)
	if !ok {
		// 尝试从 float32 转换
		if thresholdFloat32, ok := config["threshold"].(float32); ok {
			threshold = float64(thresholdFloat32)
		} else {
			threshold = 0.3 // 默认值
		}
	}
	thresholdLow := getConfigFloat32(config, "threshold_low", float32(threshold))

	// 创建TEN-VAD实例
	tenVAD := GetInstance()
	handle, err := tenVAD.CreateInstance(hopSize, float32(threshold))
	if err != nil {
		return nil, fmt.Errorf("创建TEN-VAD实例失败: %v", err)
	}

	log.Debugf("创建TEN-VAD实例成功, hopSize: %d, threshold: %f, threshold_low: %f", hopSize, threshold, thresholdLow)

	return &TenVAD{
		handle:       handle,
		hopSize:      hopSize,
		threshold:    float32(threshold),
		thresholdLow: thresholdLow,
		detector:     hysteresis.NewDetector(float32(threshold), thresholdLow),
	}, nil
}

// IsVAD 实现VAD接口的IsVAD方法
func (t *TenVAD) IsVAD(pcmData []float32) (bool, error) {
	return t.IsVADExt(pcmData, 16000, t.hopSize)
}

// IsVADExt 实现VAD接口的IsVADExt方法
func (t *TenVAD) IsVADExt(pcmData []float32, sampleRate int, frameSize int) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.handle == nil {
		return false, errors.New("TEN-VAD实例未初始化")
	}
	if t.detector == nil {
		t.detector = hysteresis.NewDetector(t.threshold, t.thresholdLow)
	}

	if len(pcmData) == 0 {
		return false, nil
	}

	// 将 float32 转换为 int16
	// float32 范围: -1.0 到 1.0
	// int16 范围: -32768 到 32767
	int16Data := make([]int16, len(pcmData))
	for i, f := range pcmData {
		// 限制范围并转换
		if f > 1.0 {
			f = 1.0
		} else if f < -1.0 {
			f = -1.0
		}
		int16Data[i] = int16(f * 32768.0)
	}

	// 按 hopSize 分帧处理
	tenVAD := GetInstance()
	hasVoice := false
	voiceFrameCount := 0

	for i := 0; i < len(int16Data); i += t.hopSize {
		end := i + t.hopSize
		if end > len(int16Data) {
			end = len(int16Data)
		}

		frame := int16Data[i:end]
		// 如果帧长度不足 hopSize，需要填充或跳过
		if len(frame) < t.hopSize {
			// 对于最后一帧，如果长度不足，可以选择跳过或填充
			// 这里选择跳过不足的帧
			continue
		}

		probability, _, err := tenVAD.ProcessAudio(t.handle, frame)
		if err != nil {
			log.Errorf("TEN-VAD处理音频帧失败: %v", err)
			continue
		}

		if t.detector.Evaluate(probability) {
			hasVoice = true
			voiceFrameCount++
		}
	}

	// 如果至少有一帧检测到语音，则认为有语音活动
	return hasVoice, nil
}

// Reset 重置VAD检测器状态
func (t *TenVAD) Reset() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.detector != nil {
		t.detector.Reset()
	}
	return nil
}

// Close 关闭并释放资源
func (t *TenVAD) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.handle != nil {
		tenVAD := GetInstance()
		err := tenVAD.DestroyInstance(t.handle)
		if err != nil {
			return fmt.Errorf("销毁TEN-VAD实例失败: %v", err)
		}
		t.handle = nil
	}
	return nil
}

// IsValid 检查资源是否有效
func (t *TenVAD) IsValid() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.handle != nil
}

// AcquireVAD 创建并返回 TEN-VAD 实例（由全局资源池管理）
func AcquireVAD(config map[string]interface{}) (VAD, error) {
	return NewTenVAD(config)
}

// ReleaseVAD 释放 VAD 实例
func ReleaseVAD(vad VAD) error {
	if vad != nil {
		return vad.Close()
	}
	return nil
}

func getConfigFloat32(config map[string]interface{}, key string, fallback float32) float32 {
	switch value := config[key].(type) {
	case float32:
		return value
	case float64:
		return float32(value)
	case int:
		return float32(value)
	case int64:
		return float32(value)
	case int32:
		return float32(value)
	}
	return fallback
}
