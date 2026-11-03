package server

import (
	"context"
	"testing"

	"xiaozhi-esp32-server-golang/internal/app/server/notify"

	"github.com/spf13/viper"
)

func TestDeriveBaseURLFromWebsocketURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "wss with reverse proxy prefix",
			in:   "wss://www.tb263.cn:55555/go_ws/xiaozhi/v1/",
			want: "https://www.tb263.cn:55555/go_ws",
		},
		{
			name: "ws direct root path",
			in:   "ws://192.168.209.168:8990/xiaozhi/v1/",
			want: "http://192.168.209.168:8990",
		},
		{
			name: "mqtt_udp ws path",
			in:   "ws://10.0.0.8:8989/xiaozhi/mqtt_udp/v1",
			want: "http://10.0.0.8:8989",
		},
		{
			name: "placeholder domain rejected",
			in:   "ws://www.youdomain.cn/xiaozhi/v1/",
			want: "",
		},
		{
			name: "invalid scheme rejected",
			in:   "ftp://192.168.1.2:21/xiaozhi/v1/",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveBaseURLFromWebsocketURL(tc.in); got != tc.want {
				t.Fatalf("deriveBaseURLFromWebsocketURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveNotifyPublicBaseURLPriority(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("websocket.port", 8990)
	viper.Set("ota.test.websocket.url", "ws://192.168.208.214:8989/xiaozhi/v1/")
	if got := resolveNotifyPublicBaseURL(); got != "http://192.168.208.214:8989" {
		t.Fatalf("expected ota.test fallback, got %q", got)
	}

	viper.Set("ota.external.websocket.url", "wss://notify.example.com:8443/proxy/xiaozhi/v1/")
	if got := resolveNotifyPublicBaseURL(); got != "https://notify.example.com:8443/proxy" {
		t.Fatalf("expected ota.external priority over ota.test, got %q", got)
	}

	viper.Set("server.public_url", "https://server.example.com/")
	if got := resolveNotifyPublicBaseURL(); got != "https://server.example.com" {
		t.Fatalf("expected server.public_url priority, got %q", got)
	}

	viper.Set("notify.public_url", "https://custom-notify.example.com/base/")
	if got := resolveNotifyPublicBaseURL(); got != "https://custom-notify.example.com/base" {
		t.Fatalf("expected notify.public_url top priority, got %q", got)
	}
}

func TestResolveNotifyTTSConfigByIDAndFallback(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("tts", map[string]interface{}{
		"tts_qwen_1": map[string]interface{}{
			"provider":   "aliyun_qwen",
			"name":       "千问默认",
			"is_default": true,
			"api_key":    "sk-test",
			"voice":      "Cherry",
		},
		"tts_cosy_2": map[string]interface{}{
			"provider":   "cosyvoice",
			"name":       "Cosy克隆",
			"is_default": false,
			"api_url":    "https://tts.example.com/tts",
			"spk_id":     "spk_default",
		},
	})

	var app *App

	// 1. 指定 TTSConfigID (大小写不敏感匹配) + 音色覆盖 (cosyvoice 写入 spk_id)
	provider, cfg, err := app.resolveNotifyTTSConfig(context.Background(), &notify.NotifyConfig{
		DeviceID:    "dev-1",
		TTSConfigID: "TTS_COSY_2",
		Voice:       "spk_custom",
		Speed:       1.2,
	})
	if err != nil {
		t.Fatalf("resolveNotifyTTSConfig failed: %v", err)
	}
	if provider != "cosyvoice" {
		t.Fatalf("expected provider cosyvoice, got %q", provider)
	}
	if cfg["spk_id"] != "spk_custom" {
		t.Fatalf("expected cosyvoice spk_id=spk_custom, got %v", cfg["spk_id"])
	}
	if _, hasVoice := cfg["voice"]; hasVoice {
		t.Fatalf("cosyvoice should not pollute voice key when overriding spk_id")
	}
	if gotVoice := extractNotifyVoiceID(provider, cfg); gotVoice != "spk_custom" {
		t.Fatalf("extractNotifyVoiceID got %q, want spk_custom", gotVoice)
	}

	// 2. 指定不存在的 TTSConfigID 应明确报错
	if _, _, err := app.resolveNotifyTTSConfig(context.Background(), &notify.NotifyConfig{
		DeviceID:    "dev-1",
		TTSConfigID: "non_existent_id",
	}); err == nil {
		t.Fatal("expected error for unknown TTSConfigID")
	}

	// 3. 未指定 TTSConfigID 且无活跃会话时，应回退到 viper 中 is_default=true 的 TTS 配置并保留默认音色用于池复用
	provider, cfg, err = app.resolveNotifyTTSConfig(context.Background(), &notify.NotifyConfig{
		DeviceID: "offline-dev",
	})
	if err != nil {
		t.Fatalf("default fallback failed: %v", err)
	}
	if provider != "aliyun_qwen" || cfg["voice"] != "Cherry" || cfg["api_key"] != "sk-test" {
		t.Fatalf("unexpected default fallback config: provider=%q cfg=%+v", provider, cfg)
	}
	if gotVoice := extractNotifyVoiceID(provider, cfg); gotVoice != "Cherry" {
		t.Fatalf("expected default voice Cherry for pool label alignment, got %q", gotVoice)
	}
}
