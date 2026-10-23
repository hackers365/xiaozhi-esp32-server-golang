package aliyun_qwen3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/constants"
	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/gorilla/websocket"
)

// AliyunQwen3ASR is the Aliyun Qwen3 ASR engine.
type AliyunQwen3ASR struct {
	config Config
	dialer *websocket.Dialer
	taskMu sync.Mutex
	connMu sync.Mutex
	conn   *websocket.Conn
}

func classifyAliyunQwen3RetryReason(err error) string {
	if err == nil {
		return types.RetryReasonNone
	}

	errText := strings.ToLower(err.Error())
	if strings.Contains(errText, "read message failed") &&
		(strings.Contains(errText, "forcibly closed by the remote host") ||
			strings.Contains(errText, "websocket: close 1006") ||
			strings.Contains(errText, "connection reset by peer") ||
			strings.Contains(errText, "broken pipe") ||
			strings.Contains(errText, "unexpected eof")) {
		return types.RetryReasonAliyunQwen3ConnectionClosed
	}

	return types.RetryReasonNone
}

// NewAliyunQwen3ASR creates a new instance.
func NewAliyunQwen3ASR(config Config) (*AliyunQwen3ASR, error) {
	if config.WsURL == "" {
		return nil, fmt.Errorf("ws_url is empty")
	}

	// Validate audio format.
	format := config.Format
	if format == "" {
		format = "pcm"
	}
	if format != "pcm" && format != "opus" {
		return nil, fmt.Errorf("aliyun qwen3 only supports pcm or opus format, got: %s", format)
	}

	// Validate sample rate.
	if config.SampleRate == 0 {
		config.SampleRate = 16000
	}
	if config.SampleRate != 8000 && config.SampleRate != 16000 {
		return nil, fmt.Errorf("aliyun qwen3 only supports 8000 or 16000 sample_rate, got: %d", config.SampleRate)
	}
	if config.SampleRate != 16000 {
		return nil, fmt.Errorf("main program currently only supports 16000 sample_rate")
	}

	// Validate language.
	if config.Language == "" {
		config.Language = "zh"
	}

	config.Format = format

	return &AliyunQwen3ASR{
		config: config,
		dialer: websocket.DefaultDialer,
	}, nil
}

// StreamingRecognize performs streaming ASR recognition.
func (a *AliyunQwen3ASR) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	a.taskMu.Lock()
	var unlockOnce sync.Once
	unlock := func() {
		unlockOnce.Do(func() {
			a.taskMu.Unlock()
		})
	}

	// Resolve API key.
	apiKey := a.config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("DASHSCOPE_API_KEY")
	}
	if apiKey == "" {
		unlock()
		return nil, fmt.Errorf("missing api key: DASHSCOPE_API_KEY is empty")
	}

	// Build WebSocket URL.
	wsURL := a.config.WsURL
	if a.config.Model != "" {
		wsURL = fmt.Sprintf("%s?model=%s", wsURL, a.config.Model)
	}

	// Build WebSocket request headers.
	header := make(http.Header)
	header.Add("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	header.Add("OpenAI-Beta", "realtime=v1")

	// Lazy connect: defer websocket dialing until the first audio chunk arrives.
	log.Debugf("[aliyun_qwen3] lazy connect enabled: websocket will be created on first audio chunk")

	resultChan := make(chan types.StreamingResult, 20)
	done := make(chan struct{})
	var closeOnce sync.Once

	closeDone := func() {
		closeOnce.Do(func() {
			close(done)
		})
	}

	var sendErrMu sync.Mutex
	var sendErr error
	var audioChunkCount int
	var totalAudioBytes int
	var sessionUpdated bool
	sessionUpdatedChan := make(chan struct{}, 1)
	var bufferCommitted bool
	bufferCommittedChan := make(chan struct{}, 1)
	var finalResultReceived bool
	finalResultChan := make(chan struct{}, 1)

	recordSendErr := func(err error) {
		if err == nil {
			return
		}
		sendErrMu.Lock()
		if sendErr == nil {
			sendErr = err
		}
		sendErrMu.Unlock()
	}

	var resultMu sync.Mutex
	var resultClosed bool
	closeResults := func() {
		resultMu.Lock()
		defer resultMu.Unlock()
		if !resultClosed {
			close(resultChan)
			resultClosed = true
		}
	}

	sendResult := func(r types.StreamingResult) {
		resultMu.Lock()
		defer resultMu.Unlock()
		if resultClosed {
			return
		}
		if !r.IsFinal {
			select {
			case resultChan <- r:
			default:
			}
			return
		}
		select {
		case resultChan <- r:
			return
		default:
			for {
				select {
				case <-resultChan:
				default:
					resultChan <- r
					return
				}
			}
		}
	}

	var conn *websocket.Conn
	connectReady := make(chan struct{})
	var connectOnce sync.Once
	var connectErr error
	var connectErrMu sync.Mutex

	ensureConnection := func() error {
		connectOnce.Do(func() {
			a.connMu.Lock()
			existing := a.conn
			a.connMu.Unlock()
			if existing != nil {
				log.Debugf("[aliyun_qwen3] reuse websocket connection")
				conn = existing
				close(connectReady)
				return
			}

			log.Debugf("[aliyun_qwen3] connecting to: %s", wsURL)
			dialConn, _, dialErr := a.dialer.DialContext(ctx, wsURL, header)
			if dialErr != nil {
				connectErrMu.Lock()
				connectErr = fmt.Errorf("connect websocket failed: %w", dialErr)
				connectErrMu.Unlock()
				return
			}
			a.connMu.Lock()
			a.conn = dialConn
			a.connMu.Unlock()
			conn = dialConn
			log.Debugf("[aliyun_qwen3] websocket connected")
			close(connectReady)
		})
		connectErrMu.Lock()
		defer connectErrMu.Unlock()
		return connectErr
	}

	// ctx cancel 时主动关闭底层连接，确保 ReadMessage 及时返回，
	// 这样旧会话一定会退出并释放 taskMu，避免下一轮重启卡死。
	go func() {
		select {
		case <-ctx.Done():
			recordSendErr(ctx.Err())
			log.Debugf("[aliyun_qwen3] context cancelled, closing websocket to unblock receiver")
			if conn != nil {
				a.resetConn(conn)
			}
		case <-done:
		}
	}()

	// Start receiver goroutine.
	go func() {
		defer closeDone()
		defer closeResults()

		sessionFinished := false

		select {
		case <-ctx.Done():
			return
		case <-connectReady:
		}
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				a.resetConn(conn)
				if !sessionFinished {
					sendErrMu.Lock()
					localErr := sendErr
					sendErrMu.Unlock()
					if localErr == nil {
						localErr = fmt.Errorf("read message failed: %w", err)
					}
					sendResult(types.StreamingResult{
						Error:       localErr,
						IsFinal:     true,
						AsrType:     constants.AsrTypeAliyunQwen3,
						RetryReason: classifyAliyunQwen3RetryReason(localErr),
					})
				}
				return
			}

			var event ServerEvent
			if err := json.Unmarshal(message, &event); err != nil {
				log.Debugf("[aliyun_qwen3] failed to parse message: %v, raw: %s", err, string(message))
				continue
			}

			// Log full incoming event.
			if jsonStr := string(message); len(jsonStr) > 500 {
				log.Debugf("[aliyun_qwen3] received event %s (truncated): %s...", event.Type, jsonStr[:500])
			} else {
				log.Debugf("[aliyun_qwen3] received event %s: %s", event.Type, jsonStr)
			}

			// Handle event types.
			switch event.Type {
			case "session.updated":
				// Session updated successfully.
				log.Debugf("[aliyun_qwen3] session.updated received")
				if !sessionUpdated {
					sessionUpdated = true
					select {
					case sessionUpdatedChan <- struct{}{}:
					default:
					}
				}

			case "input_audio_buffer.speech_started":
				// VAD detected speech start.
				log.Debugf("[aliyun_qwen3] speech_started detected")

			case "input_audio_buffer.speech_stopped":
				// VAD detected speech end.
				log.Debugf("[aliyun_qwen3] speech_stopped detected")

			case "input_audio_buffer.committed":
				// Audio buffer commit acknowledged.
				log.Debugf("[aliyun_qwen3] input_audio_buffer.committed received")
				if !bufferCommitted {
					bufferCommitted = true
					select {
					case bufferCommittedChan <- struct{}{}:
					default:
					}
				}

			case "conversation.item.created":
				// Conversation item created.
				log.Debugf("[aliyun_qwen3] conversation item created")

			case "conversation.item.input_audio_transcription.text":
				// Realtime transcription result.
				text := GetTranscriptionText(&event)
				log.Debugf("[aliyun_qwen3] transcription.text (partial): %q", text)
				if text != "" {
					sendResult(types.StreamingResult{
						Text:    text,
						IsFinal: false,
						AsrType: constants.AsrTypeAliyunQwen3,
						Mode:    "online",
					})
				}

			case "conversation.item.input_audio_transcription.completed":
				// Final transcription result.
				text := GetTranscriptionText(&event)
				log.Debugf("[aliyun_qwen3] transcription.completed (final): %q", text)
				if !finalResultReceived {
					finalResultReceived = true
					select {
					case finalResultChan <- struct{}{}:
					default:
					}
				}
				var emptyReason string
				if text == "" {
					emptyReason = types.EmptyReasonProviderEmptyFinal
				}
				sendResult(types.StreamingResult{
					Text:        text,
					IsFinal:     true,
					AsrType:     constants.AsrTypeAliyunQwen3,
					Mode:        "online",
					EmptyReason: emptyReason,
				})

			case "session.finished":
				// Session finished.
				log.Debugf("[aliyun_qwen3] session.finished received")
				sessionFinished = true
				return

			case "error":
				// Error event.
				errMsg := "unknown error"
				if event.Error != nil {
					errMsg = event.Error.Message
				}
				log.Debugf("[aliyun_qwen3] error event: %s", errMsg)
				if !finalResultReceived {
					finalResultReceived = true
					select {
					case finalResultChan <- struct{}{}:
					default:
					}
				}
				sendResult(types.StreamingResult{
					Error:   fmt.Errorf("aliyun qwen3 error: %s", errMsg),
					IsFinal: true,
					AsrType: constants.AsrTypeAliyunQwen3,
				})
				return
			default:
				// Log unhandled event type.
				log.Debugf("[aliyun_qwen3] unhandled event type: %s", event.Type)
			}
		}
	}()

	// Release the task lock after done.
	go func() {
		<-done
		unlock()
	}()

	sendSessionUpdate := func() error {
		if conn == nil {
			return fmt.Errorf("websocket connection is not ready")
		}
		updateEvent := NewSessionUpdateEvent(a.config)
		updateBytes, err := json.Marshal(updateEvent)
		if err != nil {
			return fmt.Errorf("marshal session.update failed: %w", err)
		}
		log.Debugf("[aliyun_qwen3] sending session.update, auto_end=%v", a.config.AutoEnd)
		if err := conn.WriteMessage(websocket.TextMessage, updateBytes); err != nil {
			return fmt.Errorf("send session.update failed: %w", err)
		}
		log.Debugf("[aliyun_qwen3] session.update sent")

		updateWait := a.config.Timeout
		if updateWait <= 0 {
			updateWait = 10 * time.Second
		}
		timer := time.NewTimer(updateWait)
		defer func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}()

		select {
		case <-sessionUpdatedChan:
			log.Debugf("[aliyun_qwen3] session.updated confirmed, starting sender goroutine")
			return nil
		case <-timer.C:
			return fmt.Errorf("wait session.updated timeout after %s", updateWait)
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			sendErrMu.Lock()
			localErr := sendErr
			sendErrMu.Unlock()
			if localErr == nil {
				localErr = fmt.Errorf("session ended before session.updated")
			}
			return localErr
		}
	}

	// Start sender goroutine. The websocket and session.update are created on
	// the first audio chunk to keep the lazy-connect behavior from this branch.
	select {
	case <-ctx.Done():
		log.Debugf("[aliyun_qwen3] context cancelled before sending audio")
		if conn != nil {
			a.resetConn(conn)
		}
		closeDone()
		return nil, ctx.Err()
	default:
	}
	go func() {
		defer func() {
			log.Debugf("[aliyun_qwen3] sender exiting: sent %d chunks, %d bytes total", audioChunkCount, totalAudioBytes)
		}()
		defer func() {
			// Wait for the final transcription (max 5s).
			log.Debugf("[aliyun_qwen3] waiting for final transcription...")
			waitForResult := true
			if a.config.AutoEnd {
				// VAD mode: wait for final result.
				select {
				case <-finalResultChan:
					log.Debugf("[aliyun_qwen3] final transcription received; sending session.finish")
				case <-time.After(5 * time.Second):
					log.Debugf("[aliyun_qwen3] final transcription wait timeout (5s); sending session.finish")
				case <-ctx.Done():
					log.Debugf("[aliyun_qwen3] context cancelled while waiting for final result")
					waitForResult = false
				}
			} else {
				// Manual mode: wait for final result.
				select {
				case <-finalResultChan:
					log.Debugf("[aliyun_qwen3] final transcription received; sending session.finish")
				case <-time.After(5 * time.Second):
					log.Debugf("[aliyun_qwen3] final transcription wait timeout (5s); sending session.finish")
				case <-ctx.Done():
					log.Debugf("[aliyun_qwen3] context cancelled while waiting for final result")
					waitForResult = false
				}
			}

			if !waitForResult {
				return
			}
			// Lazy connect: with no audio the websocket was never opened.
			if conn == nil || audioChunkCount == 0 {
				return
			}

			// Send session.finish.
			finishEvent := NewSessionFinishEvent()
			log.Debugf("[aliyun_qwen3] sending session.finish")
			if bytes, err := json.Marshal(finishEvent); err == nil {
				if jsonStr := string(bytes); len(jsonStr) > 300 {
					log.Debugf("[aliyun_qwen3] session.finish payload (truncated): %s...", jsonStr[:300])
				} else {
					log.Debugf("[aliyun_qwen3] session.finish payload: %s", jsonStr)
				}
				if err := conn.WriteMessage(websocket.TextMessage, bytes); err != nil {
					a.resetConn(conn)
					log.Debugf("[aliyun_qwen3] session.finish send failed: %v", err)
				} else {
					log.Debugf("[aliyun_qwen3] session.finish sent")
				}
			}
			// Wait for session.finished or timeout.
			finishWait := a.config.Timeout
			if finishWait <= 0 {
				finishWait = 10 * time.Second
			}
			log.Debugf("[aliyun_qwen3] waiting for session.finished (timeout %v)...", finishWait)
			timer := time.NewTimer(finishWait)
			select {
			case <-done:
				log.Debugf("[aliyun_qwen3] session.finished received")
			case <-timer.C:
				log.Debugf("[aliyun_qwen3] session.finished wait timeout")
			case <-ctx.Done():
				log.Debugf("[aliyun_qwen3] context cancelled while waiting for session.finished")
			}
			timer.Stop()
		}()

		for {
			select {
			case <-ctx.Done():
				recordSendErr(ctx.Err())
				return

			case pcm, ok := <-audioStream:
				if !ok {
					// Audio stream ended.
					log.Debugf("[aliyun_qwen3] audio stream ended, auto_end=%v, sent %d chunks, %d bytes", a.config.AutoEnd, audioChunkCount, totalAudioBytes)
					if audioChunkCount == 0 {
						log.Debugf("[aliyun_qwen3] no audio chunk received, skip creating websocket session")
						return
					}
					if !a.config.AutoEnd {
						// Manual mode requires input_audio_buffer.commit.
						commitEvent := NewAudioCommitEvent()
						log.Debugf("[aliyun_qwen3] sending input_audio_buffer.commit")
						if bytes, err := json.Marshal(commitEvent); err == nil {
							if jsonStr := string(bytes); len(jsonStr) > 300 {
								log.Debugf("[aliyun_qwen3] commit payload (truncated): %s...", jsonStr[:300])
							} else {
								log.Debugf("[aliyun_qwen3] commit payload: %s", jsonStr)
							}
							if err := conn.WriteMessage(websocket.TextMessage, bytes); err != nil {
								a.resetConn(conn)
								recordSendErr(fmt.Errorf("send commit failed: %w", err))
								log.Debugf("[aliyun_qwen3] commit send failed: %v", err)
							} else {
								log.Debugf("[aliyun_qwen3] commit sent; waiting for input_audio_buffer.committed")
								// Wait for input_audio_buffer.committed (max 5s).
								select {
								case <-bufferCommittedChan:
									log.Debugf("[aliyun_qwen3] input_audio_buffer.committed received")
								case <-time.After(5 * time.Second):
									log.Debugf("[aliyun_qwen3] input_audio_buffer.committed wait timeout; continue")
								case <-ctx.Done():
									log.Debugf("[aliyun_qwen3] context cancelled while waiting for buffer.committed")
									return
								}
							}
						}
					}
					log.Debugf("[aliyun_qwen3] sender goroutine exiting")
					return
				}

				// Convert audio to PCM16 bytes.
				if audioChunkCount == 0 {
					if err := ensureConnection(); err != nil {
						recordSendErr(err)
						sendResult(types.StreamingResult{
							Error:       err,
							IsFinal:     true,
							AsrType:     constants.AsrTypeAliyunQwen3,
							RetryReason: classifyAliyunQwen3RetryReason(err),
						})
						return
					}
					if err := sendSessionUpdate(); err != nil {
						recordSendErr(err)
						sendResult(types.StreamingResult{
							Error:       err,
							IsFinal:     true,
							AsrType:     constants.AsrTypeAliyunQwen3,
							RetryReason: classifyAliyunQwen3RetryReason(err),
						})
						if conn != nil {
							a.resetConn(conn)
						}
						return
					}
				}
				audioBytes := float32SliceToPCM16Bytes(pcm)
				audioChunkCount++
				totalAudioBytes += len(audioBytes)

				// Send audio append event.
				appendEvent := NewAudioAppendEvent(audioBytes)
				if bytes, err := json.Marshal(appendEvent); err != nil {
					recordSendErr(fmt.Errorf("marshal audio append failed: %w", err))
					log.Debugf("[aliyun_qwen3] marshal audio append failed: %v", err)
					return
				} else {
					if err := conn.WriteMessage(websocket.TextMessage, bytes); err != nil {
						a.resetConn(conn)
						recordSendErr(fmt.Errorf("send audio failed: %w", err))
						log.Debugf("[aliyun_qwen3] send audio failed: chunk #%d: %v", audioChunkCount, err)
						return
					}
					// Log every 10th chunk.
					/*if audioChunkCount%10 == 1 {
						log.Debugf("[aliyun_qwen3] sent audio chunk #%d (%d bytes), total %d bytes", audioChunkCount, len(audioBytes), totalAudioBytes)
					}*/
				}
			}
		}
	}()

	return resultChan, nil
}

// Process runs a single recognition using the streaming API.
func (a *AliyunQwen3ASR) Process(pcmData []float32) (string, error) {
	ctx := context.Background()
	if a.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
		defer cancel()
	}

	audioStream := make(chan []float32, 1)
	go func() {
		audioStream <- pcmData
		close(audioStream)
	}()

	resultChan, err := a.StreamingRecognize(ctx, audioStream)
	if err != nil {
		return "", err
	}

	var finalText string
	for result := range resultChan {
		if result.Error != nil {
			return "", result.Error
		}
		if result.Text != "" {
			finalText = result.Text
		}
		if result.IsFinal {
			return finalText, nil
		}
	}

	if finalText != "" {
		return finalText, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", fmt.Errorf("no asr result")
}

// Close releases resources.
func (a *AliyunQwen3ASR) Close() error {
	a.connMu.Lock()
	conn := a.conn
	a.conn = nil
	a.connMu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// IsValid reports whether the instance is usable.
func (a *AliyunQwen3ASR) IsValid() bool {
	return a != nil
}

func (a *AliyunQwen3ASR) resetConn(conn *websocket.Conn) {
	a.connMu.Lock()
	if a.conn == conn {
		_ = a.conn.Close()
		a.conn = nil
	}
	a.connMu.Unlock()
}

// float32ToInt16 converts a float32 sample to int16.
func float32ToInt16(sample float32) int16 {
	if sample > 1.0 {
		sample = 1.0
	} else if sample < -1.0 {
		sample = -1.0
	}
	return int16(sample * 32767)
}

// float32SliceToPCM16Bytes converts float32 samples to PCM16 little-endian bytes.
func float32SliceToPCM16Bytes(samples []float32) []byte {
	data := make([]byte, len(samples)*2)
	util.Float32ToPCMBytes(samples, data)
	return data
}
