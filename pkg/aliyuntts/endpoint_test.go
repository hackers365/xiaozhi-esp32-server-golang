package aliyuntts

import "testing"

func TestResolveHTTPAPIURLByModelProtocol(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		model   string
		want    string
		wantErr bool
	}{
		{
			name:   "qwen tts uses multimodal generation",
			rawURL: "https://dashscope.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer",
			model:  "qwen3-tts-flash",
			want:   "https://dashscope.aliyuncs.com" + MultimodalGenerationPath,
		},
		{
			name:   "qwen audio uses speech synthesizer",
			rawURL: "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation",
			model:  "qwen-audio-3.0-tts-flash",
			want:   "https://dashscope.aliyuncs.com" + SpeechSynthesizerPath,
		},
		{
			name:   "cosyvoice workspace endpoint",
			rawURL: "https://ws-123.cn-beijing.maas.aliyuncs.com/api/v1",
			model:  "cosyvoice-v3-flash",
			want:   "https://ws-123.cn-beijing.maas.aliyuncs.com" + SpeechSynthesizerPath,
		},
		{
			name:    "qwen audio rejects singapore",
			rawURL:  "https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation",
			model:   "qwen-audio-3.0-tts-plus",
			wantErr: true,
		},
		{
			name:   "custom compatible endpoint stays unchanged",
			rawURL: "https://tts.example.com/v1/synthesize?tenant=demo",
			model:  "qwen-audio-3.0-tts-flash",
			want:   "https://tts.example.com/v1/synthesize?tenant=demo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveHTTPAPIURL(tt.rawURL, tt.model)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveHTTPAPIURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveHTTPAPIURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
