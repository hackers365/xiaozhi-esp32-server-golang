package mqtt_udp

import (
	"context"
	"errors"
	"sync"
	"time"
	"xiaozhi-esp32-server-golang/internal/app/server/types"

	log "xiaozhi-esp32-server-golang/logger"
)

const (
	MaxIdleDuration = 300 //300s 没有上下行数据 就断开
)

// MqttUdpConn 实现 types.IConn 接口，适配 MQTT-UDP 连接
// 你可以根据实际需要扩展方法和字段

type MqttUdpConn struct {
	ctx    context.Context
	cancel context.CancelFunc

	DeviceId string

	PubTopic  string
	publisher mqttPublisher
	udpServer *UdpServer

	UdpSession *UdpSession

	recvCmdChan chan []byte
	sync.RWMutex

	data sync.Map

	onCloseCbList []func(deviceId string)

	lastActiveTs int64 //上下行 信令和音频数据 都会更新
}

// NewMqttUdpConn 创建一个新的 MqttUdpConn 实例
func NewMqttUdpConn(deviceID string, pubTopic string, publisher mqttPublisher, udpServer *UdpServer, udpSession *UdpSession) *MqttUdpConn {
	ctx, cancel := context.WithCancel(context.Background())
	log.Log().Debugf("NewMqttUdpConn pubTopic: %s", pubTopic)
	return &MqttUdpConn{
		ctx:      ctx,
		cancel:   cancel,
		DeviceId: deviceID,

		PubTopic:   pubTopic,
		publisher:  publisher,
		udpServer:  udpServer,
		UdpSession: udpSession,

		recvCmdChan: make(chan []byte, 100),

		data: sync.Map{},
	}
}

// SendCmd 通过 MQTT-UDP 发送命令（需对接实际发送逻辑）
func (c *MqttUdpConn) SendCmd(msg []byte) error {
	//log.Debugf("mqtt udp conn send cmd, topic: %s, msg: %s", c.PubTopic, string(msg))
	c.lastActiveTs = time.Now().Unix()
	c.RLock()
	publisher := c.publisher
	c.RUnlock()
	if publisher == nil {
		return errors.New("mqtt publisher is nil")
	}
	return publisher.Publish(c.PubTopic, msg)
}

func (c *MqttUdpConn) PushMsgToRecvCmd(msg []byte) error {
	select {
	case c.recvCmdChan <- msg:
		c.lastActiveTs = time.Now().Unix()
		return nil
	default:
		return errors.New("recvCmdChan is full")
	}
}

// RecvCmd 接收命令/信令数据
func (c *MqttUdpConn) RecvCmd(ctx context.Context, timeout int) ([]byte, error) {
	select {
	case <-ctx.Done():
		log.Debugf("mqtt udp conn recv cmd context done")
		return nil, ctx.Err()
	case msg := <-c.recvCmdChan:
		return msg, nil
	case <-time.After(time.Duration(timeout) * time.Second):
		log.Debugf("mqtt udp conn recv cmd timeout")
		return nil, nil
	}
}

// SendAudio 通过 MQTT-UDP 发送音频（需对接实际发送逻辑）
func (c *MqttUdpConn) SendAudio(audio []byte) error {
	ok, err := c.UdpSession.SendAudioData(audio)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("sendAudioChan is full")
	}
	return nil
	/*
		select {
		case c.UdpSession.SendChannel <- audio:
			c.lastActiveTs = time.Now().Unix()
			return nil
		default:
			return errors.New("sendAudioChan is full")
		}*/
}

// RecvAudio 接收音频数据
func (c *MqttUdpConn) RecvAudio(ctx context.Context, timeout int) ([]byte, error) {
	select {
	case <-ctx.Done():
		log.Debugf("mqtt udp conn recv audio context done")
		return nil, ctx.Err()
	case audio, ok := <-c.UdpSession.RecvChannel:
		if ok {
			c.lastActiveTs = time.Now().Unix()
			return audio, nil
		}
		return nil, errors.New("recvAudioChan is closed")
	case <-time.After(time.Duration(timeout) * time.Second):
		log.Debugf("mqtt udp conn recv audio timeout")
		return nil, nil
	}
}

// GetDeviceID 获取设备ID
func (c *MqttUdpConn) GetDeviceID() string {
	return c.DeviceId
}

// Close 关闭连接
func (c *MqttUdpConn) Close() error {
	//c.cancel()
	c.Destroy()
	return nil
}

func (c *MqttUdpConn) OnClose(closeCb func(deviceId string)) {
	c.onCloseCbList = append(c.onCloseCbList, closeCb)
}

func (c *MqttUdpConn) SetPublisher(publisher mqttPublisher) {
	c.Lock()
	c.publisher = publisher
	c.Unlock()
}

func (c *MqttUdpConn) GetTransportType() string {
	return types.TransportTypeMqttUdp
}

func (c *MqttUdpConn) SetData(key string, value interface{}) {
	c.data.Store(key, value)
}

func (c *MqttUdpConn) GetData(key string) (interface{}, error) {
	value, ok := c.data.Load(key)
	if !ok {
		return nil, errors.New("key not found")
	}
	return value, nil
}

func (c *MqttUdpConn) IsActive() bool {
	return time.Now().Unix()-c.lastActiveTs < MaxIdleDuration
}

// 销毁
func (c *MqttUdpConn) Destroy() {
	c.cancel()
	for _, cb := range c.onCloseCbList {
		cb(c.DeviceId)
	}
}

func (c *MqttUdpConn) CloseAudioChannel() error {
	return nil
}
