package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao/request"
	"xiaozhi-esp32-server-golang/internal/domain/asr/doubao/response"
	"xiaozhi-esp32-server-golang/internal/util"

	log "xiaozhi-esp32-server-golang/logger"
)

type AsrWsClient struct {
	seq        int
	url        string
	connect    *websocket.Conn
	appId      string
	accessKey  string
	resourceID string
	mu         sync.RWMutex // Protects connect from concurrent access

	// 延迟连接相关字段
	connectOnce  sync.Once     // 确保连接只建立一次
	connectReady chan struct{} // 通知接收 goroutine 连接已建立
	connectErr   error         // 连接建立时的错误
	connectErrMu sync.Mutex    // 保护 connectErr
}

func NewAsrWsClient(url string, appKey, accessKey, resourceID string) *AsrWsClient {
	return &AsrWsClient{
		seq:          1,
		url:          url,
		appId:        appKey,
		accessKey:    accessKey,
		resourceID:   resourceID,
		connectReady: make(chan struct{}),
	}
}

func (c *AsrWsClient) CreateConnection(ctx context.Context) error {
	header := request.NewAuthHeader(c.appId, c.accessKey, c.resourceID)
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, c.url, header)
	if err != nil {
		if resp != nil {
			var body string
			if resp.Body != nil {
				bodyBytes, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if readErr == nil {
					body = string(bodyBytes)
				}
			}
			return fmt.Errorf("dial websocket err: %w, status=%d, body=%s", err, resp.StatusCode, body)
		}
		return fmt.Errorf("dial websocket err: %w", err)
	}
	_ = resp
	//log.Debugf("logid: %s", resp.Header.Get("X-Tt-Logid"))
	c.mu.Lock()
	c.connect = conn
	c.mu.Unlock()
	return nil
}

func (c *AsrWsClient) SendFullClientRequest() error {
	c.mu.RLock()
	conn := c.connect
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("websocket connection is nil")
	}

	fullClientRequest := request.NewFullClientRequest()
	c.seq++
	err := conn.WriteMessage(websocket.BinaryMessage, fullClientRequest)
	if err != nil {
		return fmt.Errorf("full client message write websocket err: %w", err)
	}
	_, resp, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("full client message read err: %w", err)
	}
	_ = resp
	//respStruct := response.ParseResponse(resp)
	//log.Println(respStruct)
	return nil
}

// ensureConnection 确保连接已建立（延迟连接，带重试机制）
func (c *AsrWsClient) ensureConnection(ctx context.Context) error {
	var err error
	c.connectOnce.Do(func() {
		log.Debugf("延迟建立连接：收到第一个音频包，开始建立连接")

		// 重试配置
		const (
			maxRetries = 3                      // 最大重试次数（总共尝试4次：初始1次 + 重试3次）
			retryDelay = 500 * time.Millisecond // 重试延迟
		)

		for attempt := 1; attempt <= maxRetries+1; attempt++ {
			// 尝试建立连接
			err = c.CreateConnection(ctx)
			if err != nil {
				if attempt <= maxRetries {
					log.Warnf("延迟建立连接失败(第%d次): %v，%v后重试", attempt, err, retryDelay)
					select {
					case <-ctx.Done():
						err = fmt.Errorf("连接建立被取消: %w", ctx.Err())
						c.connectErrMu.Lock()
						c.connectErr = err
						c.connectErrMu.Unlock()
						return
					case <-time.After(retryDelay):
						// 固定延迟后重试
					}
					continue
				} else {
					// 最后一次重试失败
					log.Errorf("延迟建立连接失败(第%d次，已达最大重试次数): %v", attempt, err)
					c.connectErrMu.Lock()
					c.connectErr = err
					c.connectErrMu.Unlock()
					return
				}
			}

			// 连接建立成功，发送初始化请求
			err = c.SendFullClientRequest()
			if err != nil {
				// 发送初始化请求失败，关闭连接并重试
				log.Warnf("发送初始化请求失败(第%d次): %v", attempt, err)
				c.Close()

				if attempt <= maxRetries {
					log.Warnf("%v后重试建立连接", retryDelay)
					select {
					case <-ctx.Done():
						err = fmt.Errorf("连接建立被取消: %w", ctx.Err())
						c.connectErrMu.Lock()
						c.connectErr = err
						c.connectErrMu.Unlock()
						return
					case <-time.After(retryDelay):
						// 固定延迟后重试
					}
					continue
				} else {
					// 最后一次重试失败
					log.Errorf("发送初始化请求失败(第%d次，已达最大重试次数): %v", attempt, err)
					c.connectErrMu.Lock()
					c.connectErr = err
					c.connectErrMu.Unlock()
					return
				}
			}

			// 连接和初始化都成功
			if attempt > 1 {
				log.Infof("延迟建立连接成功(第%d次尝试)", attempt)
			} else {
				log.Debugf("延迟建立连接成功")
			}
			// 通知接收 goroutine 连接已建立
			close(c.connectReady)
			return
		}
	})
	return err
}

func (c *AsrWsClient) SendMessages(ctx context.Context, audioStream <-chan []float32, stopChan <-chan struct{}) error {
	messageChan := make(chan []byte)
	go func() {
		for message := range messageChan {
			c.mu.RLock()
			conn := c.connect
			c.mu.RUnlock()

			if conn == nil {
				log.Debugf("websocket connection is nil, stopping message writer")
				return
			}

			err := conn.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				log.Debugf("write message err: %s", err)
				return
			}
		}
	}()

	defer close(messageChan)
	firstPacket := true
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("send messages context done")
		case <-stopChan:
			return fmt.Errorf("send messages stop chan")
		case audioData, ok := <-audioStream:
			if !ok {
				log.Debugf("sendMessages audioStream closed")
				// 如果连接未建立（静音情况），直接返回
				c.mu.RLock()
				conn := c.connect
				c.mu.RUnlock()
				if conn == nil {
					log.Debugf("audioStream 关闭且连接未建立，直接返回（静音情况）")
					return nil
				}
				// 连接已建立，发送结束消息
				endMessage := request.NewAudioOnlyRequest(-c.seq, []byte{})
				messageChan <- endMessage
				return nil
			}

			// 收到第一个音频包时，建立连接
			if firstPacket {
				firstPacket = false
				err := c.ensureConnection(ctx)
				if err != nil {
					log.Errorf("建立连接失败: %v", err)
					return fmt.Errorf("ensure connection err: %w", err)
				}
			}

			byteData := make([]byte, len(audioData)*2)
			util.Float32ToPCMBytes(audioData, byteData)
			message := request.NewAudioOnlyRequest(c.seq, byteData)
			messageChan <- message
			c.seq++
		}
	}
}

func (c *AsrWsClient) recvMessages(ctx context.Context, resChan chan<- *response.AsrResponse, stopChan chan<- struct{}) {
	for {
		c.mu.RLock()
		conn := c.connect
		c.mu.RUnlock()

		if conn == nil {
			log.Debugf("websocket connection is nil, stopping message receiver")
			return
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		resp := response.ParseResponse(message)
		select {
		case <-ctx.Done():
			return
		case resChan <- resp:
		}
		if resp.IsLastPackage {
			return
		}
		if resp.Code != 0 {
			close(stopChan)
			return
		}
	}
}

func (c *AsrWsClient) StartAudioStream(ctx context.Context, audioStream <-chan []float32, resChan chan<- *response.AsrResponse) error {
	stopChan := make(chan struct{})
	sendDoneChan := make(chan error, 1) // 发送完成通知（nil表示正常完成，error表示出错）

	// 启动发送 goroutine
	go func() {
		err := c.SendMessages(ctx, audioStream, stopChan)
		// 无论成功还是失败，都发送通知
		sendDoneChan <- err
	}()

	// 等待连接建立或发送完成
	select {
	case <-ctx.Done():
		return fmt.Errorf("start audio stream context done")
	case <-c.connectReady:
		// 连接已建立，启动接收 goroutine
		log.Debugf("连接已建立，启动接收 goroutine")
		c.recvMessages(ctx, resChan, stopChan)
		return nil
	case err := <-sendDoneChan:
		// 发送完成（可能是正常完成或出错）
		if err != nil {
			// 发送过程中出错
			log.Errorf("发送音频流失败: %v", err)
			return err
		}
		// 检查是否是静音情况（连接未建立）
		c.mu.RLock()
		conn := c.connect
		c.mu.RUnlock()
		if conn == nil {
			// 静音情况：audioStream 关闭但连接未建立
			log.Debugf("静音情况：连接未建立，发送空结果")
			payload := &response.AsrResponsePayload{}
			payload.Result.Text = ""
			resChan <- &response.AsrResponse{
				Code:          0,
				IsLastPackage: true,
				PayloadMsg:    payload,
			}
			return nil
		}
		// 连接已建立，启动接收 goroutine（处理剩余的响应）
		c.recvMessages(ctx, resChan, stopChan)
		return nil
	}
}

func (c *AsrWsClient) Excute(ctx context.Context, audioStream chan []float32, resChan chan<- *response.AsrResponse) error {
	c.seq = 1
	if c.url == "" {
		return errors.New("url is empty")
	}
	err := c.CreateConnection(ctx)
	if err != nil {
		return fmt.Errorf("create connection err: %w", err)
	}
	err = c.SendFullClientRequest()
	if err != nil {
		return fmt.Errorf("send full request err: %w", err)
	}

	err = c.StartAudioStream(ctx, audioStream, resChan)
	if err != nil {
		return fmt.Errorf("start audio stream err: %w", err)
	}
	return nil
}

func (c *AsrWsClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connect != nil {
		err := c.connect.Close()
		c.connect = nil
		return err
	}
	return nil
}
