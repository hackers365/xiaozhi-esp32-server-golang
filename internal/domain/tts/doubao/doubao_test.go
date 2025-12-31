package doubao

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"xiaozhi-esp32-server-golang/internal/util"
)

// 测试创建TTS提供者
func TestNewDoubaoTTSProvider(t *testing.T) {
	config := map[string]interface{}{
		"appid":         "test_app_id",
		"access_token":  "test_token",
		"cluster":       "test_cluster",
		"voice":         "test_voice",
		"api_url":       "https://api.test.com",
		"authorization": "Bearer ",
	}

	provider := NewDoubaoTTSProvider(config)

	if provider.AppID != "test_app_id" {
		t.Errorf("AppID不匹配，期望: %s, 实际: %s", "test_app_id", provider.AppID)
	}
	if provider.Voice != "test_voice" {
		t.Errorf("Voice不匹配，期望: %s, 实际: %s", "test_voice", provider.Voice)
	}
	if provider.Header["Authorization"] != "Bearer test_token" {
		t.Errorf("Authorization不匹配，期望: %s, 实际: %s", "Bearer test_token", provider.Header["Authorization"])
	}
}

// 测试GetVoiceInfo方法
func TestGetVoiceInfo(t *testing.T) {
	provider := &DoubaoTTSProvider{
		Voice: "xiaomei",
	}

	info := provider.GetVoiceInfo()
	if info["voice"] != "xiaomei" {
		t.Errorf("语音信息不匹配，期望voice: %s, 实际: %s", "xiaomei", info["voice"])
	}
	if info["type"] != "doubao" {
		t.Errorf("类型不匹配，期望type: %s, 实际: %s", "doubao", info["type"])
	}
}

// 测试生成UUID
func TestGenerateUUID(t *testing.T) {
	uuid := generateUUID()
	if len(uuid) == 0 {
		t.Error("生成的UUID为空")
	}

	// 测试多次生成的UUID是否不同
	anotherUUID := generateUUID()
	if uuid == anotherUUID {
		t.Error("两次生成的UUID相同，期望不同的值")
	}
}

// 注意：以下是一个简化的WavToOpus测试
// 如果要全面测试，需要准备有效的WAV数据并验证转换结果
func TestWavToOpus_InvalidData(t *testing.T) {
	// 测试无效的WAV数据
	// 使用标准音频参数：sampleRate=16000, channels=1, bitRate=64000
	_, err := util.WavToOpus([]byte("这不是WAV数据"), 16000, 1, 64000)
	if err == nil {
		t.Error("期望处理无效数据时返回错误，但没有")
	}
}

// MockRoundTripper 模拟HTTP请求的响应
type MockRoundTripper struct {
	Response *http.Response
	Err      error
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.Response, m.Err
}

// 使用字节跳动配置进行测试
func TestByteProviderTextToSpeech(t *testing.T) {
	// 配置测试参数 - 使用用户提供的实际配置
	config := map[string]interface{}{
		"api_url":       "https://openspeech.bytedance.com/api/v1/tts",
		"voice":         "BV001_streaming",
		"authorization": "Bearer;",
		"appid":         "6886011847",
		"access_token":  "access_token",
		"cluster":       "volcano_tts",
	}

	t.Logf("使用配置: API URL=%s, Voice=%s, AppID=%s",
		config["api_url"], config["voice"], config["appid"])

	// 创建TTS提供者
	provider := NewDoubaoTTSProvider(config)

	// 测试文本到语音转换
	testText := "这是一个测试，使用字节跳动TTS服务生成语音"
	t.Logf("测试文本: %s", testText)

	// 确保输出目录存在
	outputDir := "tmp/"
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Logf("无法创建输出目录: %v", err)
		}
	}

	// 使用TextToSpeech方法
	// 使用标准音频参数：sampleRate=16000, channels=1, frameDuration=20
	audioFrames, err := provider.TextToSpeech(context.Background(), testText, 16000, 1, 20)
	if err != nil {
		t.Fatalf("TextToSpeech失败: %v", err)
	}

	// 计算总大小
	var totalSize int
	for _, frame := range audioFrames {
		totalSize += len(frame)
	}

	// 合并所有帧以便保存到文件
	mergedAudio := make([]byte, totalSize)
	offset := 0
	for _, frame := range audioFrames {
		copy(mergedAudio[offset:], frame)
		offset += len(frame)
	}

	// 保存结果
	outputPath := outputDir + "byte_test_" + time.Now().Format("20060102_150405") + ".opus"
	if err := os.WriteFile(outputPath, mergedAudio, 0644); err != nil {
		t.Logf("保存音频文件失败: %v", err)
	} else {
		t.Logf("音频文件已保存到: %s", outputPath)
	}

	// 验证结果
	if len(audioFrames) == 0 {
		t.Error("生成的音频帧为空")
	} else {
		t.Logf("生成的音频帧数量: %d, 总大小: %d 字节", len(audioFrames), totalSize)
	}
}
