package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	types_audio "xiaozhi-esp32-server-golang/internal/data/audio"
	. "xiaozhi-esp32-server-golang/internal/data/client"
	. "xiaozhi-esp32-server-golang/internal/data/msg"
	log "xiaozhi-esp32-server-golang/logger"
)

// ServerTransport handles sending messages to the client via the transport layer
// (原ServerMsgService)
type ServerTransport struct {
	transport      types_conn.IConn
	clientState    *ClientState
	McpRecvMsgChan chan []byte
	closed         bool
	mu             sync.Mutex
}

func NewServerTransport(transport types_conn.IConn, clientState *ClientState) *ServerTransport {
	return &ServerTransport{
		transport:      transport,
		clientState:    clientState,
		McpRecvMsgChan: make(chan []byte, 100),
	}
}

func (s *ServerTransport) SendTtsStart() error {
	msg := ServerMessage{
		Type:      ServerMessageTypeTts,
		State:     MessageStateStart,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	s.clientState.SetTtsStart(true)
	return nil
}

func (s *ServerTransport) SendTtsStop() error {
	msg := ServerMessage{
		Type:      ServerMessageTypeTts,
		State:     MessageStateStop,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	s.clientState.IsWelcomePlaying = false
	s.clientState.SetTtsStart(false)
	// 一轮对话播报结束后，回到可触发下一轮对话的状态。
	s.clientState.SetStatus(ClientStatusListenStop)
	return nil
}

func (s *ServerTransport) SendMqttGoodbye() error {
	msg := ServerMessage{
		Type:      ServerMessageTypeGoodBye,
		State:     MessageStateStop,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServerTransport) SendHello(transportType string, audioFormat *types_audio.AudioFormat, udpConfig *UdpConfig) error {
	msg := ServerMessage{
		Type:        MessageTypeHello,
		Text:        "欢迎使用小智服务器",
		SessionID:   s.clientState.SessionID,
		Transport:   transportType,
		AudioFormat: audioFormat,
		Udp:         udpConfig,
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServerTransport) SendIot(msg *ClientMessage) error {
	resp := ServerMessage{
		Type:      ServerMessageTypeIot,
		Text:      msg.Text,
		SessionID: s.clientState.SessionID,
		State:     MessageStateSuccess,
	}
	bytes, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServerTransport) SendAsrResult(text string) error {
	resp := ServerMessage{
		Type:      ServerMessageTypeStt,
		Text:      text,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServerTransport) SendSentenceStart(text string) error {
	response := ServerMessage{
		Type:      ServerMessageTypeTts,
		State:     MessageStateSentenceStart,
		Text:      text,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	s.clientState.SetStatus(ClientStatusTTSStart)
	return nil
}

func (s *ServerTransport) SendSentenceEnd(text string) error {
	response := ServerMessage{
		Type:      ServerMessageTypeTts,
		State:     MessageStateSentenceEnd,
		Text:      text,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	s.clientState.SetStatus(ClientStatusTTSStart)
	return nil
}

func (s *ServerTransport) SendCmd(cmdBytes []byte) error {
	return s.transport.SendCmd(cmdBytes)
}

func (s *ServerTransport) SendAudio(audio []byte) error {
	return s.transport.SendAudio(audio)
}

func (s *ServerTransport) GetTransportType() string {
	return s.transport.GetTransportType()
}

func (s *ServerTransport) GetData(key string) (interface{}, error) {
	return s.transport.GetData(key)
}

func (s *ServerTransport) SendMcpMsg(payload []byte) error {
	response := ServerMessage{
		Type:      MessageTypeMcp,
		SessionID: s.clientState.SessionID,
		PayLoad:   payload,
	}
	bytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	err = s.transport.SendCmd(bytes)
	if err != nil {
		return err
	}
	return nil
}

func (s *ServerTransport) RecvMcpMsg(ctx context.Context, timeOut int) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-s.McpRecvMsgChan:
		if !ok {
			return nil, fmt.Errorf("transport is closed")
		}
		return msg, nil
	case <-time.After(time.Duration(timeOut) * time.Millisecond):
		return nil, fmt.Errorf("mcp 接收消息超时")
	}
}

func (s *ServerTransport) HandleMcpMessage(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("transport is closed")
	}
	select {
	case s.McpRecvMsgChan <- payload:
	default:
		log.Warnf("mcp 接收消息通道已满, 丢弃消息")
	}
	return nil
}

func (s *ServerTransport) IsClosed() bool {
	return s.closed
}

func (s *ServerTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil // Already closed
	}

	s.closed = true

	if s.transport.GetTransportType() == types_conn.TransportTypeMqttUdp {
		s.SendMqttGoodbye()
	}

	close(s.McpRecvMsgChan)
	return s.transport.Close()
}

func (s *ServerTransport) RecvAudio(ctx context.Context, timeOut int) ([]byte, error) {
	return s.transport.RecvAudio(ctx, timeOut)
}

func (s *ServerTransport) RecvCmd(ctx context.Context, timeOut int) ([]byte, error) {
	return s.transport.RecvCmd(ctx, timeOut)
}
