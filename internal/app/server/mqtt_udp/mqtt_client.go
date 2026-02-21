package mqtt_udp

import (
	"errors"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"xiaozhi-esp32-server-golang/internal/app/mqtt_server"
)

const (
	mqttProviderNetwork = "network"
	mqttProviderInline  = "inline"
)

type mqttPublisher interface {
	Publish(topic string, payload []byte) error
}

type pahoPublisher struct {
	client mqtt.Client
}

func (p *pahoPublisher) Publish(topic string, payload []byte) error {
	if p == nil || p.client == nil {
		return errors.New("mqtt client is nil")
	}
	token := p.client.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
}

type inlinePublisher struct{}

func (p *inlinePublisher) Publish(topic string, payload []byte) error {
	srv := mqtt_server.GetCurrentServer()
	if srv == nil {
		return errors.New("mqtt_server is not running")
	}
	return srv.Publish(topic, payload, false, 0)
}

// normalizeMqttType 将 mqtt.type 归一化为内部模式：
// embed => 进程内直连；其它 => 网络客户端。
func normalizeMqttType(connType string) string {
	t := strings.ToLower(strings.TrimSpace(connType))
	switch t {
	case "embed":
		return mqttProviderInline
	default:
		return mqttProviderNetwork
	}
}
