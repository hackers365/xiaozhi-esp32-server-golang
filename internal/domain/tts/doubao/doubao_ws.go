package doubao

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/doubaoapi"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gorilla/websocket"
)

var wsDialer = websocket.Dialer{
	ReadBufferSize:   16384,
	WriteBufferSize:  16384,
	HandshakeTimeout: 45 * time.Second,
}

var doubaoWSRequestHeader = []byte{0x11, 0x10, 0x11, 0x00}

const (
	doubaoWSEventSessionFinished  int32 = 152
	doubaoWSEventSessionFailed    int32 = 153
	doubaoWSEventTTSSentenceStart int32 = 350
	doubaoWSEventTTSSentenceEnd   int32 = 351
	doubaoWSEventTTSSubtitle      int32 = 364
)

type DoubaoWSProvider struct {
	APIKey      string
	AppID       string
	AccessToken string
	Model       string
	ResourceID  string
	Voice       string
	WSURL       string
}

type doubaoWSAttemptResult struct {
	outputChan chan []byte
	err        error
}

func NewDoubaoWSProvider(config map[string]interface{}) *DoubaoWSProvider {
	apiKey, _ := config["api_key"].(string)
	appID, _ := config["appid"].(string)
	accessToken, _ := config["access_token"].(string)
	model, _ := config["model"].(string)
	resourceID, _ := config["resource_id"].(string)
	voice, _ := config["voice"].(string)
	wsURL, _ := config["ws_url"].(string)
	if strings.TrimSpace(wsURL) == "" {
		if wsHost, ok := config["ws_host"].(string); ok && strings.TrimSpace(wsHost) != "" {
			host := strings.TrimSpace(wsHost)
			if !strings.Contains(host, "://") {
				wsURL = "wss://" + strings.TrimLeft(host, "/")
			} else {
				wsURL = host
			}
			wsURL = strings.TrimRight(wsURL, "/") + "/api/v3/tts/unidirectional/stream"
		}
	}
	return &DoubaoWSProvider{
		APIKey:      strings.TrimSpace(apiKey),
		AppID:       appID,
		AccessToken: accessToken,
		Model:       normalizeDoubaoModel(model),
		ResourceID:  strings.TrimSpace(resourceID),
		Voice:       strings.TrimSpace(voice),
		WSURL:       normalizeDoubaoWSURL(wsURL),
	}
}

func normalizeDoubaoWSURL(raw string) string {
	wsURL := normalizeDoubaoTTSURL(raw, defaultDoubaoWSURL)
	wsURL = strings.TrimSpace(wsURL)
	if strings.HasSuffix(wsURL, "/api/v3/tts/unidirectional") {
		return wsURL + "/stream"
	}
	return wsURL
}

func (p *DoubaoWSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	outputChan, err := p.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
	if err != nil {
		return nil, err
	}
	frames := make([][]byte, 0, 32)
	for frame := range outputChan {
		if len(frame) > 0 {
			frames = append(frames, frame)
		}
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("豆包 WebSocket TTS 返回音频为空")
	}
	return frames, nil
}

func (p *DoubaoWSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	voice := strings.TrimSpace(p.Voice)
	if voice == "" {
		return nil, fmt.Errorf("豆包 WebSocket TTS 缺少 voice")
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	derivedResolved, err := resolveDoubaoTTSModel(p.Model, voice)
	if err != nil {
		return nil, err
	}
	explicitResourceID := strings.TrimSpace(p.ResourceID)
	if sampleRate <= 0 {
		sampleRate = defaultDoubaoSampleHz
	}
	tryResolved := buildDoubaoWSAttemptModels(derivedResolved, explicitResourceID, voice)
	attemptedResources := make([]string, 0, len(tryResolved))
	attemptErrors := make([]error, 0, len(tryResolved))

	for idx, candidate := range tryResolved {
		attemptedResources = append(attemptedResources, candidate.ResourceID)

		outputChan, attemptErr := p.textToSpeechStreamWithModel(ctx, text, sampleRate, frameDuration, candidate)
		if attemptErr == nil {
			return outputChan, nil
		}
		attemptErrors = append(attemptErrors, attemptErr)
		if idx == len(tryResolved)-1 || !isDoubaoRetryableResourceError(attemptErr) {
			return nil, summarizeDoubaoWSAttemptError(voice, tryResolved[0], attemptedResources, attemptErrors)
		}
		log.Warnf("豆包 WebSocket TTS 资源族不匹配，尝试切换重试: voice=%s from=%s to=%s", voice, candidate.ResourceID, tryResolved[idx+1].ResourceID)
	}

	return nil, fmt.Errorf("豆包 WebSocket TTS 未找到可用的资源族")
}

func (p *DoubaoWSProvider) textToSpeechStreamWithModel(ctx context.Context, text string, sampleRate int, frameDuration int, resolved resolvedTTSModel) (chan []byte, error) {
	voice := strings.TrimSpace(p.Voice)
	reqBody := newDoubaoWSPayload(text, voice, sampleRate, resolved.RequestModel)
	requestFrame, err := buildDoubaoWSBinaryRequest(reqBody)
	if err != nil {
		return nil, fmt.Errorf("构建豆包 WebSocket TTS 请求失败: %w", err)
	}

	headers := doubaoapi.NewTTSWebsocketHeadersWithAuth(p.APIKey, p.AppID, p.AccessToken, resolved.ResourceID, doubaoapi.NewConnectID())
	conn, resp, err := wsDialer.DialContext(ctx, p.WSURL, headers)
	if err != nil {
		return nil, formatDoubaoWSConnectError(err, resp)
	}

	outputChan := make(chan []byte, 1000)
	pipeReader, pipeWriter := io.Pipe()
	attemptResult := make(chan doubaoWSAttemptResult, 1)
	startTs := time.Now().UnixMilli()
	decoder, err := util.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, defaultDoubaoAudioFmt, sampleRate)
	if err != nil {
		_ = conn.Close()
		_ = pipeReader.Close()
		_ = pipeWriter.Close()
		close(outputChan)
		return nil, fmt.Errorf("创建豆包 WebSocket 音频解码器失败: %w", err)
	}
	go func() {
		if err := decoder.Run(startTs); err != nil {
			log.Errorf("豆包 WebSocket 音频解码失败: %v", err)
		}
	}()

	go func() {
		defer conn.Close()
		defer pipeWriter.Close()

		if err := conn.WriteMessage(websocket.BinaryMessage, requestFrame); err != nil {
			trySendDoubaoWSAttemptResult(attemptResult, doubaoWSAttemptResult{err: fmt.Errorf("发送豆包 WebSocket TTS 请求失败: %w", err)})
			_ = pipeWriter.CloseWithError(fmt.Errorf("发送豆包 WebSocket TTS 请求失败: %w", err))
			return
		}

		audioReceived := false
		for {
			_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					if !audioReceived {
						trySendDoubaoWSAttemptResult(attemptResult, doubaoWSAttemptResult{err: fmt.Errorf("豆包 WebSocket TTS 连接已关闭但未收到音频")})
					}
					return
				}
				trySendDoubaoWSAttemptResult(attemptResult, doubaoWSAttemptResult{err: fmt.Errorf("读取豆包 WebSocket TTS 响应失败: %w", err)})
				_ = pipeWriter.CloseWithError(fmt.Errorf("读取豆包 WebSocket TTS 响应失败: %w", err))
				return
			}
			if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
				continue
			}
			log.Debugf("豆包 WebSocket TTS 收到消息: ws_type=%d payload_len=%d", messageType, len(payload))
			audioChunk, isLast, err := parseDoubaoWSMessage(messageType, payload)
			if err != nil {
				trySendDoubaoWSAttemptResult(attemptResult, doubaoWSAttemptResult{err: err})
				_ = pipeWriter.CloseWithError(err)
				return
			}
			if len(audioChunk) > 0 {
				if !audioReceived {
					audioReceived = true
					trySendDoubaoWSAttemptResult(attemptResult, doubaoWSAttemptResult{outputChan: outputChan})
				}
				if _, err := pipeWriter.Write(audioChunk); err != nil {
					trySendDoubaoWSAttemptResult(attemptResult, doubaoWSAttemptResult{err: err})
					_ = pipeWriter.CloseWithError(err)
					return
				}
			}
			if isLast {
				if !audioReceived {
					trySendDoubaoWSAttemptResult(attemptResult, doubaoWSAttemptResult{err: fmt.Errorf("豆包 WebSocket TTS 未收到有效音频")})
				}
				return
			}
		}
	}()

	select {
	case result := <-attemptResult:
		if result.err != nil {
			return nil, result.err
		}
		return result.outputChan, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func trySendDoubaoWSAttemptResult(ch chan doubaoWSAttemptResult, result doubaoWSAttemptResult) {
	select {
	case ch <- result:
	default:
	}
}

func (p *DoubaoWSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	if voice, ok := voiceConfig["voice"].(string); ok && strings.TrimSpace(voice) != "" {
		p.Voice = strings.TrimSpace(voice)
	}
	if model, ok := voiceConfig["model"].(string); ok && strings.TrimSpace(model) != "" {
		p.Model = normalizeDoubaoModel(model)
	}
	if resourceID, ok := voiceConfig["resource_id"].(string); ok {
		p.ResourceID = strings.TrimSpace(resourceID)
	}
	return nil
}

func (p *DoubaoWSProvider) Close() error {
	return nil
}

func (p *DoubaoWSProvider) IsValid() bool {
	return p != nil
}

func newDoubaoWSPayload(text, speaker string, sampleRate int, requestModel string) doubaoTTSV3Request {
	return newDoubaoTTSV3Request(text, speaker, sampleRate, requestModel)
}

func buildDoubaoWSBinaryRequest(req any) ([]byte, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化豆包 WebSocket TTS 请求失败: %w", err)
	}
	compressed, err := gzipCompressDoubao(payload)
	if err != nil {
		return nil, fmt.Errorf("压缩豆包 WebSocket TTS 请求失败: %w", err)
	}

	sizeBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(sizeBuf, uint32(len(compressed)))

	frame := make([]byte, 0, len(doubaoWSRequestHeader)+len(sizeBuf)+len(compressed))
	frame = append(frame, doubaoWSRequestHeader...)
	frame = append(frame, sizeBuf...)
	frame = append(frame, compressed...)
	return frame, nil
}

func parseDoubaoWSMessage(messageType int, payload []byte) ([]byte, bool, error) {
	if len(payload) == 0 {
		return nil, false, nil
	}

	if looksLikeJSONPayload(payload) {
		var directErr map[string]any
		if err := json.Unmarshal(payload, &directErr); err == nil {
			if errMsg, ok := directErr["error"].(string); ok && strings.TrimSpace(errMsg) != "" {
				return nil, false, fmt.Errorf("%s", strings.TrimSpace(errMsg))
			}
		}
	}

	if messageType == websocket.TextMessage || looksLikeJSONPayload(payload) {
		event, err := parseDoubaoTTSStreamEvent(payload)
		if err != nil || event == nil {
			return nil, false, err
		}
		chunk, err := decodeDoubaoTTSAudioChunk(event)
		if err != nil {
			return nil, false, err
		}
		return chunk, event.Code == doubaoTTSStreamDoneCode || event.Sequence < 0, nil
	}

	return parseDoubaoWSBinaryResponse(payload)
}

func parseDoubaoWSBinaryResponse(frame []byte) ([]byte, bool, error) {
	if len(frame) < 4 {
		return nil, false, fmt.Errorf("解析豆包 WebSocket TTS 响应失败: 响应帧长度不足")
	}

	headerSizeBytes := int(frame[0]&0x0f) * 4
	if headerSizeBytes <= 0 || len(frame) < headerSizeBytes {
		return nil, false, fmt.Errorf("解析豆包 WebSocket TTS 响应失败: 非法头部长度")
	}

	messageType := frame[1] >> 4
	flags := frame[1] & 0x0f
	compression := frame[2] & 0x0f
	payload := frame[headerSizeBytes:]

	switch messageType {
	case 0x09:
		log.Debugf("豆包 WebSocket TTS 收到 full server response: flags=%d compression=%d payload_len=%d", flags, compression, len(payload))
		return parseDoubaoWSFullServerResponse(payload, compression, flags)
	case 0x0b:
		log.Debugf("豆包 WebSocket TTS 收到 audio-only response: flags=%d payload_len=%d", flags, len(payload))
		if flags == 0 {
			return nil, false, nil
		}
		if flags == 4 {
			_, requestID, audio, err := extractDoubaoWSFlag4Payload(payload)
			if err != nil {
				return nil, false, err
			}
			log.Debugf("豆包 WebSocket 音频帧(request_id=%s): audio_len=%d", requestID, len(audio))
			return audio, false, nil
		}
		if len(payload) < 8 {
			return nil, false, fmt.Errorf("解析豆包 WebSocket 音频帧失败: payload 长度不足")
		}
		sequenceNumber := int32(binary.BigEndian.Uint32(payload[0:4]))
		payloadSize := int(binary.BigEndian.Uint32(payload[4:8]))
		audio := payload[8:]
		if payloadSize >= 0 && payloadSize < len(audio) {
			audio = audio[:payloadSize]
		}
		return audio, sequenceNumber < 0, nil
	case 0x0c:
		if len(payload) < 4 {
			return nil, false, fmt.Errorf("解析豆包 WebSocket 前端消息失败: payload 长度不足")
		}
		msgPayload := payload[4:]
		if compression == 1 {
			var err error
			msgPayload, err = gzipDecompressDoubao(msgPayload)
			if err != nil {
				return nil, false, fmt.Errorf("解压豆包 WebSocket 前端消息失败: %w", err)
			}
		}
		if !looksLikeJSONPayload(msgPayload) {
			return nil, false, nil
		}
		event, err := parseDoubaoTTSStreamEvent(msgPayload)
		if err != nil || event == nil {
			return nil, false, err
		}
		chunk, err := decodeDoubaoTTSAudioChunk(event)
		if err != nil {
			return nil, false, err
		}
		return chunk, event.Code == doubaoTTSStreamDoneCode || event.Sequence < 0, nil
	case 0x0f:
		if len(payload) < 8 {
			return nil, false, fmt.Errorf("解析豆包 WebSocket 错误帧失败: payload 长度不足")
		}
		code := int32(binary.BigEndian.Uint32(payload[0:4]))
		errPayload := payload[8:]
		if compression == 1 {
			var err error
			errPayload, err = gzipDecompressDoubao(errPayload)
			if err != nil {
				return nil, false, fmt.Errorf("解压豆包 WebSocket 错误帧失败: %w", err)
			}
		}
		msg := strings.TrimSpace(string(errPayload))
		if msg == "" {
			msg = fmt.Sprintf("豆包 WebSocket TTS 服务端错误(code=%d)", code)
		}
		return nil, false, fmt.Errorf("%s", msg)
	default:
		preview := frame
		if len(preview) > 16 {
			preview = preview[:16]
		}
		return nil, false, fmt.Errorf("解析豆包 WebSocket TTS 响应失败: 未知消息类型 %d, 首包=%x", messageType, preview)
	}
}

func parseDoubaoWSFullServerResponse(payload []byte, compression byte, flags byte) ([]byte, bool, error) {
	if flags == 4 {
		marker, requestID, body, err := extractDoubaoWSFlag4Payload(payload)
		if err != nil {
			return nil, false, err
		}
		if compression == 1 && len(body) > 0 {
			body, err = gzipDecompressDoubao(body)
			if err != nil {
				return nil, false, fmt.Errorf("解压豆包 WebSocket flags=4 ACK 帧失败: %w", err)
			}
		}
		trimmed := bytes.TrimSpace(body)
		if marker == doubaoWSEventSessionFinished {
			log.Debugf("豆包 WebSocket ACK(request_id=%s) 会话结束: marker=%d body=%s", requestID, marker, string(trimmed))
			return nil, true, nil
		}
		if marker == doubaoWSEventSessionFailed {
			msg := strings.TrimSpace(string(trimmed))
			if msg == "" {
				msg = fmt.Sprintf("豆包 WebSocket TTS 会话失败(event=%d)", marker)
			}
			return nil, false, fmt.Errorf("%s", msg)
		}
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
			if marker == doubaoWSEventTTSSentenceStart || marker == doubaoWSEventTTSSentenceEnd || marker == doubaoWSEventTTSSubtitle {
				log.Debugf("豆包 WebSocket ACK(request_id=%s) 空事件元数据: marker=%d", requestID, marker)
				return nil, false, nil
			}
			log.Debugf("豆包 WebSocket ACK(request_id=%s) 结束标记: marker=%d body=%q", requestID, marker, string(trimmed))
			return nil, true, nil
		}
		if !looksLikeJSONPayload(trimmed) {
			log.Debugf("豆包 WebSocket ACK(request_id=%s) 非JSON payload: marker=%d body=%x", requestID, marker, trimmed)
			return nil, false, nil
		}

		var event doubaoTTSV3Event
		if err := json.Unmarshal(trimmed, &event); err == nil && (event.Code != 0 || event.Message != "" || event.Data != nil) {
			if event.Code != 0 && event.Code != doubaoTTSStreamDoneCode {
				msg := strings.TrimSpace(event.Message)
				if msg == "" {
					msg = fmt.Sprintf("豆包 WebSocket TTS 返回错误码 %d", event.Code)
				}
				return nil, false, fmt.Errorf("%s", msg)
			}
			chunk, err := decodeDoubaoTTSAudioChunk(&event)
			if err != nil {
				return nil, false, err
			}
			return chunk, event.Code == doubaoTTSStreamDoneCode || event.Sequence < 0, nil
		}

		log.Debugf("豆包 WebSocket ACK(request_id=%s) 元数据: marker=%d body=%s", requestID, marker, string(trimmed))
		return nil, false, nil
	}

	if len(payload) < 8 {
		return nil, false, fmt.Errorf("解析豆包 WebSocket ACK 帧失败: payload 长度不足")
	}

	sequenceNumber := int32(binary.BigEndian.Uint32(payload[0:4]))
	payloadSize := int(binary.BigEndian.Uint32(payload[4:8]))
	body := payload[8:]
	if payloadSize >= 0 && payloadSize < len(body) {
		body = body[:payloadSize]
	}
	if compression == 1 {
		var err error
		body, err = gzipDecompressDoubao(body)
		if err != nil {
			return nil, false, fmt.Errorf("解压豆包 WebSocket ACK 帧失败: %w", err)
		}
	}
	if !looksLikeJSONPayload(body) {
		log.Debugf("豆包 WebSocket ACK 非JSON payload: %x", body)
		return nil, sequenceNumber < 0, nil
	}

	var event doubaoTTSV3Event
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, false, fmt.Errorf("解析豆包 WebSocket ACK JSON 失败: %w", err)
	}
	log.Debugf("豆包 WebSocket ACK JSON: code=%d sequence=%d message=%q", event.Code, event.Sequence, event.Message)
	if event.Code != 0 {
		msg := strings.TrimSpace(event.Message)
		if msg == "" {
			msg = fmt.Sprintf("豆包 WebSocket TTS 返回错误码 %d", event.Code)
		}
		return nil, false, fmt.Errorf("%s", msg)
	}
	chunk, err := decodeDoubaoTTSAudioChunk(&event)
	if err != nil {
		return nil, false, err
	}
	return chunk, event.Sequence < 0 || sequenceNumber < 0, nil
}

func extractDoubaoWSFlag4Payload(payload []byte) (int32, string, []byte, error) {
	if len(payload) < 12 {
		return 0, "", nil, fmt.Errorf("解析豆包 WebSocket flags=4 帧失败: payload 长度不足")
	}
	marker := int32(binary.BigEndian.Uint32(payload[0:4]))
	requestIDLen := int(binary.BigEndian.Uint32(payload[4:8]))
	if requestIDLen < 0 || len(payload) < 8+requestIDLen+4 {
		return 0, "", nil, fmt.Errorf("解析豆包 WebSocket flags=4 帧失败: request_id 长度非法")
	}
	requestID := string(payload[8 : 8+requestIDLen])
	bodyLenOffset := 8 + requestIDLen
	bodyLen := int(binary.BigEndian.Uint32(payload[bodyLenOffset : bodyLenOffset+4]))
	body := payload[bodyLenOffset+4:]
	if bodyLen >= 0 && bodyLen <= len(body) {
		body = body[:bodyLen]
	}
	return marker, requestID, body, nil
}

func buildDoubaoPublicFallbackModel(resolved resolvedTTSModel, voice string) *resolvedTTSModel {
	if isDoubaoCloneVoice(voice) {
		return nil
	}
	switch resolved.ResourceID {
	case resourceSeedTTS10:
		fallback := resolvedTTSModel{
			ConfigModel:  modelSeedTTS20Standard,
			RequestModel: modelSeedTTS20Standard,
			ResourceID:   resourceSeedTTS20,
			VoiceFamily:  resolved.VoiceFamily,
		}
		return &fallback
	case resourceSeedTTS20:
		fallback := resolvedTTSModel{
			ConfigModel:  modelSeedTTS11,
			RequestModel: modelSeedTTS11,
			ResourceID:   resourceSeedTTS10,
			VoiceFamily:  resolved.VoiceFamily,
		}
		return &fallback
	default:
		return nil
	}
}

func buildDoubaoWSAttemptModels(derived resolvedTTSModel, explicitResourceID, voice string) []resolvedTTSModel {
	models := make([]resolvedTTSModel, 0, 3)
	seen := map[string]struct{}{}
	push := func(candidate resolvedTTSModel) {
		key := candidate.ResourceID + "|" + candidate.RequestModel
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		models = append(models, candidate)
	}

	if explicit := strings.TrimSpace(explicitResourceID); explicit != "" {
		override := derived
		override.ResourceID = explicit
		push(override)
	}
	push(derived)
	if fallback := buildDoubaoPublicFallbackModel(derived, voice); fallback != nil {
		push(*fallback)
	}
	return models
}

func formatDoubaoWSConnectError(err error, resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("建立豆包 WebSocket TTS 连接失败: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyText := strings.TrimSpace(string(body))
	if readErr != nil && bodyText == "" {
		return fmt.Errorf("建立豆包 WebSocket TTS 连接失败: websocket handshake status=%d", resp.StatusCode)
	}
	if bodyText != "" {
		return fmt.Errorf("建立豆包 WebSocket TTS 连接失败: websocket handshake status=%d body=%s", resp.StatusCode, bodyText)
	}
	return fmt.Errorf("建立豆包 WebSocket TTS 连接失败: websocket handshake status=%d", resp.StatusCode)
}

func isDoubaoResourceMismatchError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "resource id is mismatched with speaker related resource")
}

func isDoubaoResourceNotGrantedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "requested resource not granted")
}

func isDoubaoRetryableResourceError(err error) bool {
	return isDoubaoResourceMismatchError(err) || isDoubaoResourceNotGrantedError(err)
}

func summarizeDoubaoWSAttemptError(voice string, preferred resolvedTTSModel, attemptedResources []string, attemptErrors []error) error {
	if len(attemptErrors) == 0 {
		return nil
	}

	lastErr := attemptErrors[len(attemptErrors)-1]
	hasMismatch := false
	hasNotGranted := false
	for _, attemptErr := range attemptErrors {
		hasMismatch = hasMismatch || isDoubaoResourceMismatchError(attemptErr)
		hasNotGranted = hasNotGranted || isDoubaoResourceNotGrantedError(attemptErr)
	}

	retryInfo := ""
	if len(attemptedResources) > 0 {
		retryInfo = fmt.Sprintf("，已尝试 resource_id=%s", strings.Join(attemptedResources, ","))
	}

	if hasNotGranted && hasMismatch {
		return fmt.Errorf(
			"豆包 WebSocket TTS 配置不可用: voice=%s, model=%s, resource_id=%s%s。当前显式 resource_id 对当前凭据（API Key 或 AppID/AccessToken）未授权，回退到模型关联的通用 resource 后仍提示音色与资源不匹配。请以火山控制台中实际开通的音色/ResourceID 对应关系为准",
			voice,
			preferred.ConfigModel,
			preferred.ResourceID,
			retryInfo,
		)
	}
	if hasNotGranted {
		return fmt.Errorf(
			"豆包 WebSocket TTS resource_id 未授权: voice=%s, model=%s, resource_id=%s%s。当前凭据（API Key 或 AppID/AccessToken）无权访问该 ResourceID，请确认火山控制台已开通对应服务，或清空 resource_id 走模型默认映射",
			voice,
			preferred.ConfigModel,
			preferred.ResourceID,
			retryInfo,
		)
	}
	if !hasMismatch {
		return lastErr
	}

	hint := "所选音色与当前凭据可访问的 v3 ResourceID 不匹配。旧版 v1 音色列表不会自动无缝迁移到 v3。"
	switch strings.ToLower(strings.TrimSpace(voice)) {
	case "zh_female_wanwanxiaohe_moon_bigtts", "zh_female_qinqienvsheng_moon_bigtts":
		hint = "所选音色来自旧版静态列表，升级到 v3 后通常需要重新换成你账号下实际开通的音色，旧数据不会自动无缝迁移。"
	}

	return fmt.Errorf(
		"豆包 WebSocket TTS 音色与资源不匹配: voice=%s, model=%s, resource_id=%s%s。%s 请以火山控制台或账号实际开通的音色/ResourceID 对应关系为准",
		voice,
		preferred.ConfigModel,
		preferred.ResourceID,
		retryInfo,
		hint,
	)
}

func gzipCompressDoubao(input []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(input); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func gzipDecompressDoubao(input []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func looksLikeJSONPayload(payload []byte) bool {
	trimmed := bytes.TrimSpace(payload)
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
}
