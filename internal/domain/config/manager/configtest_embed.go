package manager

import (
	"bytes"
	_ "embed"

	"github.com/go-audio/wav"
)

// embeddedConfigTestZhWav 来源于 asr_server/test/asr/test_wavs/zh.wav。
//
//go:embed testdata/zh.wav
var embeddedConfigTestZhWav []byte

// loadEmbeddedConfigTestWav 加载内置测试 WAV 为 float32 PCM。
func loadEmbeddedConfigTestWav() ([]float32, error) {
	if len(embeddedConfigTestZhWav) == 0 {
		return nil, nil
	}
	return decodeWavToPCM(wav.NewDecoder(bytes.NewReader(embeddedConfigTestZhWav)))
}
