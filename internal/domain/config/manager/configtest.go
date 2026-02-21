package manager

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"xiaozhi-esp32-server-golang/internal/domain/asr"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/domain/vad/inter"
	"xiaozhi-esp32-server-golang/internal/pool"
	log "xiaozhi-esp32-server-golang/logger"
)

// DefaultTestWavPath 配置测试用固定 WAV 路径（16kHz 单声道，约 1–3 秒），可选
const DefaultTestWavPath = "testdata/config_test.wav"

// DefaultTestText LLM/TTS 固定测试文本
const DefaultTestText = "配置测试"

// loadTestWav 加载固定 WAV 为 float32 PCM，若文件不存在则返回 nil 与 nil error（调用方用 fallbackPCM）
func loadTestWav(path string) ([]float32, error) {
	if path == "" {
		path = DefaultTestWavPath
	}

	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		return decodeWavToPCM(wav.NewDecoder(f))
	}

	// 外部测试文件不可用时，回退到内置的真实语音样本（internal/domain/config/manager/testdata/zh.wav）。
	embeddedPCM, err := loadEmbeddedConfigTestWav()
	if err != nil {
		return nil, err
	}
	if len(embeddedPCM) > 0 {
		log.Debugf("[config_test] 使用内置测试 WAV: internal/domain/config/manager/testdata/zh.wav")
		return embeddedPCM, nil
	}

	return nil, nil
}

// decodeWavToPCM 将 WAV 解码为 float32 PCM；无效输入返回 nil,nil。
func decodeWavToPCM(dec *wav.Decoder) ([]float32, error) {
	if dec == nil {
		return nil, nil
	}
	if !dec.IsValidFile() {
		return nil, nil
	}
	dec.ReadInfo()
	wavFmt := dec.Format()
	frameSize := int(wavFmt.SampleRate) * 20 / 1000 * wavFmt.NumChannels
	buf := &audio.IntBuffer{Format: wavFmt, SourceBitDepth: 16, Data: make([]int, frameSize)}
	var out []float32
	for {
		n, err := dec.PCMBuffer(buf)
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil {
			return nil, err
		}
		for i := 0; i < n; i++ {
			out = append(out, float32(buf.Data[i])/32767.0)
		}
	}
	return out, nil
}

// requireAsrServerEmbed 确保 embed 模式所需的 asr_server 已初始化。
func requireAsrServerEmbed() error {
	if asr.IsAsrServerEmbedInitialized() {
		return nil
	}
	configPath := ""
	if f := flag.Lookup("asr-config"); f != nil {
		if v := strings.TrimSpace(f.Value.String()); v != "" {
			configPath = v
		}
	}
	return asr.InitAsrServerEmbed(configPath)
}

// RunConfigTest 根据下发的 data（与实时配置一致）执行 VAD/ASR/LLM/TTS 轻量测试，返回每类按 config_id 的结果
func RunConfigTest(data map[string]interface{}, testText string) (vadResult, asrResult, llmResult, ttsResult map[string]interface{}) {
	vadResult = make(map[string]interface{})
	asrResult = make(map[string]interface{})
	llmResult = make(map[string]interface{})
	ttsResult = make(map[string]interface{})

	if testText == "" {
		testText = DefaultTestText
	}
	log.Debugf("[config_test] RunConfigTest 开始 test_text=%q data.keys=%v", testText, mapKeys(data))
	// 打印接收到的各类型 config_id 及脱敏后的配置内容，便于 debug
	for _, typ := range []string{"vad", "asr", "llm", "tts"} {
		v, _ := data[typ].(map[string]interface{})
		if v == nil {
			continue
		}
		ids := make([]string, 0, len(v))
		for k := range v {
			ids = append(ids, k)
		}
		log.Debugf("[config_test] 收到 data[%s] config_ids=%v", typ, ids)
	}
	if redacted := redactSensitive(data); redacted != nil {
		if b, err := json.Marshal(redacted); err == nil {
			log.Debugf("[config_test] 收到 data 脱敏后: %s", string(b))
		}
	}

	pcm, _ := loadTestWav("")
	log.Debugf("[config_test] 使用 PCM 数据: len=%d", len(pcm))

	// VAD：统计处理耗时（从调用 IsVAD 到返回）
	if v, ok := data["vad"].(map[string]interface{}); ok {
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				vadResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				continue
			}
			wrapper, err := pool.Acquire[inter.VAD]("vad", configID, cfg)
			if err != nil {
				vadResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				continue
			}
			vad := wrapper.GetProvider()
			t0 := time.Now()
			_, err = vad.IsVAD(pcm[:min(320, len(pcm))])
			elapsedMs := time.Since(t0).Milliseconds()
			pool.Release(wrapper)
			if err != nil {
				vadResult[configID] = map[string]interface{}{"ok": false, "message": err.Error(), "first_packet_ms": elapsedMs}
			} else {
				vadResult[configID] = map[string]interface{}{"ok": true, "message": "通过", "first_packet_ms": elapsedMs}
			}
		}
	}

	// ASR：使用 StreamingRecognize 做轻量测试，统计整体耗时
	if v, ok := data["asr"].(map[string]interface{}); ok {
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				asrResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				log.Debugf("[config_test] ASR config_id=%s 配置格式无效", configID)
				continue
			}
			// 资源池 creator 需要引擎类型（funasr/doubao），用 config_id 会报「不支持的ASR引擎类型」
			asrEngineType := "funasr"
			if p, ok := cfg["provider"].(string); ok && p != "" {
				asrEngineType = p
			}
			if strings.EqualFold(asrEngineType, "embed") {
				if err := requireAsrServerEmbed(); err != nil {
					asrResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
					continue
				}
			}
			wrapper, err := pool.Acquire[asr.AsrProvider]("asr", asrEngineType, cfg)
			if err != nil {
				asrResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				continue
			}
			asrProvider := wrapper.GetProvider()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			audioCh := make(chan []float32)
			go func() {
				const chunk = 3200 // 约 200ms @ 16kHz
				for i := 0; i < len(pcm); i += chunk {
					end := i + chunk
					if end > len(pcm) {
						end = len(pcm)
					}
					audioCh <- pcm[i:end]
				}
				close(audioCh)
			}()
			t0 := time.Now()
			resultChan, err := asrProvider.StreamingRecognize(ctx, audioCh)
			pool.Release(wrapper)
			if err != nil {
				cancel()
				asrResult[configID] = map[string]interface{}{"ok": false, "message": err.Error(), "first_packet_ms": time.Since(t0).Milliseconds()}
				continue
			}
			var asrErr error
			for r := range resultChan {
				if r.Error != nil {
					asrErr = r.Error
					break
				}
			}
			elapsedMs := time.Since(t0).Milliseconds()
			cancel()
			if asrErr != nil {
				asrResult[configID] = map[string]interface{}{"ok": false, "message": asrErr.Error(), "first_packet_ms": elapsedMs}
			} else {
				asrResult[configID] = map[string]interface{}{"ok": true, "message": "通过", "first_packet_ms": elapsedMs}
			}
		}
	}

	// LLM
	if v, ok := data["llm"].(map[string]interface{}); ok {
		n := 0
		for k := range v {
			if k != "provider" {
				n++
			}
		}
		log.Debugf("[config_test] LLM 待测 config 数: %d", n)
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				llmResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				log.Debugf("[config_test] LLM config_id=%s 配置格式无效", configID)
				continue
			}
			wrapper, err := pool.Acquire[llm.LLMProvider]("llm", configID, cfg)
			if err != nil {
				llmResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				log.Debugf("[config_test] LLM config_id=%s Acquire 失败: %v", configID, err)
				continue
			}
			llmProvider := wrapper.GetProvider()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			t0 := time.Now()
			msgChan := llmProvider.ResponseWithContext(ctx, "config_test", []*schema.Message{
				{Role: "user", Content: testText},
			}, nil)
			var gotMessage bool
			var firstMsg *schema.Message
			var firstPacketMs int64
			for msg := range msgChan {
				if msg != nil {
					firstMsg = msg
					gotMessage = true
					firstPacketMs = time.Since(t0).Milliseconds()
					break
				}
			}
			cancel()
			pool.Release(wrapper)
			resultBase := map[string]interface{}{"first_packet_ms": firstPacketMs}
			if gotMessage && llm.IsLLMErrorMessage(firstMsg) {
				errMsg := llm.LLMErrorMessage(firstMsg)
				resultBase["ok"] = false
				resultBase["message"] = errMsg
				llmResult[configID] = resultBase
				log.Debugf("[config_test] LLM config_id=%s 失败(透传错误): %s", configID, errMsg)
			} else if gotMessage {
				resultBase["ok"] = true
				resultBase["message"] = "通过"
				llmResult[configID] = resultBase
				log.Debugf("[config_test] LLM config_id=%s 通过", configID)
			} else if ctx.Err() == context.DeadlineExceeded {
				resultBase["ok"] = false
				resultBase["message"] = "超时"
				llmResult[configID] = resultBase
				log.Debugf("[config_test] LLM config_id=%s 超时", configID)
			} else {
				resultBase["ok"] = false
				resultBase["message"] = "未收到响应或调用失败"
				llmResult[configID] = resultBase
				log.Debugf("[config_test] LLM config_id=%s 失败(未收到响应)", configID)
			}
		}
	} else {
		log.Debugf("[config_test] LLM data.llm 缺失或非 map, ok=%v", ok)
	}

	// TTS
	if v, ok := data["tts"].(map[string]interface{}); ok {
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				ttsResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				continue
			}
			wrapper, err := pool.Acquire[tts.TTSProvider]("tts", configID, cfg)
			if err != nil {
				ttsResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				continue
			}
			ttsProvider := wrapper.GetProvider()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			outputChan, err := ttsProvider.TextToSpeechStream(ctx, testText, 24000, 1, 60)
			if err != nil {
				cancel()
				pool.Release(wrapper)
				ttsResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				log.Warnf("TTS config test %s: %v", configID, err)
				continue
			}
			t0 := time.Now()
			var totalBytes int
			var firstPacketMs int64 = -1
			for chunk := range outputChan {
				if chunk != nil {
					if firstPacketMs < 0 {
						firstPacketMs = time.Since(t0).Milliseconds()
					}
					totalBytes += len(chunk)
				}
			}
			cancel()
			pool.Release(wrapper)
			if firstPacketMs < 0 {
				firstPacketMs = time.Since(t0).Milliseconds()
			}
			if totalBytes == 0 {
				ttsResult[configID] = map[string]interface{}{"ok": false, "message": "未收到有效音频或合成失败", "first_packet_ms": firstPacketMs}
				log.Debugf("[config_test] TTS config_id=%s 失败(未收到有效音频)", configID)
			} else {
				ttsResult[configID] = map[string]interface{}{"ok": true, "message": "通过", "first_packet_ms": firstPacketMs}
			}
		}
	}

	return vadResult, asrResult, llmResult, ttsResult
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// mapKeys 返回 map 的键列表，用于 debug 日志
func mapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// 敏感字段名（小写），脱敏后用于日志
var sensitiveKeys = map[string]bool{
	"api_key": true, "access_token": true, "token": true, "password": true, "secret": true,
}

// redactSensitive 深拷贝 data 并将敏感字段值替换为 "***"，用于 debug 日志
func redactSensitive(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	out := make(map[string]interface{}, len(data))
	for k, v := range data {
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, val := range x {
			if sensitiveKeys[strings.ToLower(k)] {
				m[k] = "***"
			} else {
				m[k] = redactValue(val)
			}
		}
		return m
	case []interface{}:
		arr := make([]interface{}, len(x))
		for i, val := range x {
			arr[i] = redactValue(val)
		}
		return arr
	default:
		return v
	}
}
