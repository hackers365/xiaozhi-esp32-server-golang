package doubao

import (
	"context"
	"fmt"
	"testing"
)

// 测试TextToSpeechStream方法
func TestTextToSpeechStream(t *testing.T) {
	// 使用实际的配置
	config := map[string]interface{}{
		"appid":        "appid",
		"access_token": "access_token",
		"cluster":      "volcano_tts",
		//"voice":        "BV001_streaming",
		"voice":      "zh_female_wanwanxiaohe_moon_bigtts",
		"ws_host":    "openspeech.bytedance.com",
		"use_stream": true,
	}

	// 创建一个测试provider
	provider := NewDoubaoWSProvider(config)

	t.Run("测试正常流式回调", func(t *testing.T) {

		// 直接调用实际的API
		// 使用标准音频参数：sampleRate=16000, channels=1, frameDuration=20
		outputOpusChan, err := provider.TextToSpeechStream(context.Background(), "这是一个测试文本，今天天气怎么样, 今天天气真好, 你是中国人, 咱们去北京天津玩好不好, 北京有什么好玩的，天津之眼吧", 16000, 1, 20)
		if err != nil {
			t.Fatalf("TextToSpeechStream返回错误: %v", err)
		}

		for opusFrame := range outputOpusChan {
			fmt.Printf("收到opus帧: %d\n", len(opusFrame))
		}

	})
	/*
		t.Run("测试取消功能", func(t *testing.T) {
			var (
				receivedChunks [][]byte
				mu             sync.Mutex
			)

			// 回调函数
			onChunk := func(chunkData []byte, isLast bool) error {
				mu.Lock()
				defer mu.Unlock()

				if chunkData != nil {
					receivedChunks = append(receivedChunks, chunkData)
				}

				return nil
			}

			// 直接调用实际的API
			cancelFunc, err := provider.TextToSpeechStream("另一个测试文本", onChunk)
			if err != nil {
				t.Fatalf("TextToSpeechStream返回错误: %v", err)
			}

			// 等待一小段时间后取消
			time.Sleep(500 * time.Millisecond)
			cancelFunc()

			// 等待一小段时间让取消生效
			time.Sleep(500 * time.Millisecond)

			// 验证结果
			mu.Lock()
			defer mu.Unlock()

			// 因为取消了处理，所以收到的块数应该是有限的
			t.Logf("取消后接收到 %d 个音频块", len(receivedChunks))
		})
	*/
}
