//go:build asr_server && cgo

package controllers

import (
	"bytes"
	"context"
	"fmt"

	asrconfig "voice_server/config"
	asrcore "voice_server/core"
	"voice_server/server"
	"xiaozhi/manager/backend/config"

	"github.com/go-audio/wav"
)

type embeddedSpeakerServiceClient struct {
	engineProvider *server.EngineProvider
}

func newEmbeddedSpeakerServiceClient(_ config.SpeakerServiceConfig) (speakerServiceClient, error) {
	provider := server.SharedEngineProvider()
	if provider == nil {
		return nil, fmt.Errorf("内嵌 asr_server 引擎提供者未初始化")
	}
	return &embeddedSpeakerServiceClient{engineProvider: provider}, nil
}

func (c *embeddedSpeakerServiceClient) Mode() string {
	return speakerServiceModeEmbed
}

func (c *embeddedSpeakerServiceClient) getEngine() (asrcore.Engine, error) {
	if c == nil || c.engineProvider == nil {
		return nil, fmt.Errorf("embedded core engine provider 未初始化")
	}
	engine, err := c.engineProvider.Engine()
	if err != nil {
		return nil, fmt.Errorf("内嵌 asr_server 未初始化: %w", err)
	}
	if !engine.HasSpeakerService() {
		return nil, fmt.Errorf("内嵌声纹服务未初始化")
	}
	return engine, nil
}

func (c *embeddedSpeakerServiceClient) Register(_ context.Context, req speakerRegisterRequest) error {
	engine, err := c.getEngine()
	if err != nil {
		return err
	}
	audioData, sampleRate, err := parseSpeakerWAVBytes(req.AudioData)
	if err != nil {
		return err
	}
	return engine.RegisterSpeaker(
		req.UserID,
		fmt.Sprintf("%d", req.AgentID),
		req.SpeakerID,
		req.SpeakerName,
		req.UUID,
		audioData,
		sampleRate,
	)
}

func (c *embeddedSpeakerServiceClient) Verify(_ context.Context, req speakerVerifyRequest) (*VerifyResult, error) {
	engine, err := c.getEngine()
	if err != nil {
		return nil, err
	}
	audioData, sampleRate, err := parseSpeakerWAVBytes(req.AudioData)
	if err != nil {
		return nil, err
	}
	result, err := engine.VerifySpeaker(
		req.UserID,
		fmt.Sprintf("%d", req.AgentID),
		req.SpeakerID,
		audioData,
		sampleRate,
	)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	return &VerifyResult{
		SpeakerID:   result.SpeakerID,
		SpeakerName: result.SpeakerName,
		Verified:    result.Verified,
		Confidence:  result.Confidence,
		Threshold:   result.Threshold,
	}, nil
}

func (c *embeddedSpeakerServiceClient) Delete(_ context.Context, req speakerDeleteRequest) error {
	engine, err := c.getEngine()
	if err != nil {
		return err
	}
	agentID := fmt.Sprintf("%d", req.AgentID)
	if req.UUID != "" {
		return engine.DeleteSpeakerByUUID(req.UserID, agentID, req.UUID)
	}
	return engine.DeleteSpeaker(req.UserID, agentID, req.SpeakerID)
}

func parseSpeakerWAVBytes(wavData []byte) ([]float32, int, error) {
	if len(wavData) == 0 {
		return nil, 0, fmt.Errorf("audio file is required")
	}

	decoder := wav.NewDecoder(bytes.NewReader(wavData))
	if !decoder.IsValidFile() {
		return nil, 0, fmt.Errorf("invalid WAV file")
	}

	sampleRate := int(decoder.SampleRate)
	numChannels := int(decoder.NumChans)
	if numChannels <= 0 || numChannels > 2 {
		return nil, 0, fmt.Errorf("unsupported number of channels: %d", numChannels)
	}

	buffer, err := decoder.FullPCMBuffer()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decode audio: %w", err)
	}

	normalizeFactor := asrconfig.GlobalConfig.Audio.NormalizeFactor
	if normalizeFactor <= 0 {
		normalizeFactor = 32768
	}

	samples := make([]float32, len(buffer.Data))
	for i, sample := range buffer.Data {
		samples[i] = float32(sample) / normalizeFactor
	}

	if numChannels == 2 {
		monoSamples := make([]float32, len(samples)/2)
		for i := 0; i < len(monoSamples); i++ {
			monoSamples[i] = (samples[i*2] + samples[i*2+1]) / 2
		}
		samples = monoSamples
	}

	return samples, sampleRate, nil
}
