package silero_vad

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"xiaozhi-esp32-server-golang/internal/domain/vad/hysteresis"
	. "xiaozhi-esp32-server-golang/internal/domain/vad/inter"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/hackers365/silero-vad-go/speech"
)

var defaultVADConfig = map[string]interface{}{
	"threshold":               0.5,
	"threshold_low":           0.5,
	"min_silence_duration_ms": 100,
	"sample_rate":             16000,
	"channels":                1,
	"speech_pad_ms":           60,
	"intra_op_num_threads":    1,
	"inter_op_num_threads":    1,
}

const (
	sourceSileroVADModelPath  = "config/models/vad/silero_vad.onnx"
	releaseSileroVADModelPath = "models/vad/silero_vad.onnx"
)

type runtimeKey struct {
	modelPath         string
	logLevel          speech.LogLevel
	numSessions       int
	intraOpNumThreads int
	interOpNumThreads int
}

type sharedRuntime struct {
	runtime *speech.Runtime
	refs    int
}

var (
	runtimeMu sync.Mutex
	runtimes  = map[runtimeKey]*sharedRuntime{}
)

// SileroVAD owns one per-audio-stream state object and shares ONNX Runtime
// sessions with other SileroVAD instances using the same runtime config.
type SileroVAD struct {
	runtime    *speech.Runtime
	runtimeKey runtimeKey
	stream     *speech.Stream

	vadThreshold     float32
	vadThresholdLow  float32
	silenceThreshold int
	speechPadMs      int
	sampleRate       int
	channels         int

	pending   []float32
	lastVoice bool
	detector  *hysteresis.Detector
	closed    bool
	mu        sync.Mutex
}

func NewSileroVAD(config map[string]interface{}) (*SileroVAD, error) {
	cfg := withDefaults(config)
	modelPath := getString(cfg, "model_path", "")
	if modelPath == "" {
		return nil, errors.New("缺少模型路径配置")
	}
	resolvedModelPath, err := resolveModelPath(modelPath)
	if err != nil {
		return nil, err
	}
	modelPath = resolvedModelPath

	threshold := getFloat32(cfg, "threshold", 0.5)
	thresholdLow := getFloat32(cfg, "threshold_low", threshold)
	silenceMs := getDurationMs(cfg, "min_silence_duration_ms", "min_silence_duration", 100)
	sampleRate := getInt(cfg, "sample_rate", 16000)
	channels := getInt(cfg, "channels", 1)
	speechPadMs := getInt(cfg, "speech_pad_ms", 60)
	numSessions := resolveNumSessions(cfg)
	intraThreads := getInt(cfg, "intra_op_num_threads", 1)
	interThreads := getInt(cfg, "inter_op_num_threads", 1)

	if channels <= 0 {
		return nil, fmt.Errorf("invalid channels: %d", channels)
	}
	if intraThreads <= 0 {
		intraThreads = 1
	}
	if interThreads <= 0 {
		interThreads = 1
	}

	rtCfg := speech.RuntimeConfig{
		ModelPath:         modelPath,
		LogLevel:          speech.LogLevelWarn,
		NumSessions:       numSessions,
		IntraOpNumThreads: intraThreads,
		InterOpNumThreads: interThreads,
	}
	key, rt, err := acquireRuntime(rtCfg)
	if err != nil {
		return nil, err
	}

	stream, err := rt.NewStream(speech.StreamConfig{
		SampleRate:           sampleRate,
		Threshold:            threshold,
		MinSilenceDurationMs: silenceMs,
		SpeechPadMs:          speechPadMs,
	})
	if err != nil {
		_ = releaseRuntime(key)
		return nil, err
	}

	return &SileroVAD{
		runtime:          rt,
		runtimeKey:       key,
		stream:           stream,
		vadThreshold:     threshold,
		vadThresholdLow:  thresholdLow,
		silenceThreshold: silenceMs,
		speechPadMs:      speechPadMs,
		sampleRate:       sampleRate,
		channels:         channels,
		detector:         hysteresis.NewDetector(threshold, thresholdLow),
	}, nil
}

func (s *SileroVAD) IsVAD(pcmData []float32) (bool, error) {
	return s.IsVADExt(pcmData, s.sampleRate, len(pcmData))
}

func (s *SileroVAD) IsVADExt(pcmData []float32, sampleRate int, frameSize int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.stream == nil || s.runtime == nil {
		return false, errors.New("Silero VAD实例未初始化")
	}
	if s.detector == nil {
		s.detector = hysteresis.NewDetector(s.vadThreshold, s.vadThresholdLow)
	}
	if len(pcmData) == 0 {
		return false, nil
	}
	if sampleRate == 0 {
		sampleRate = s.sampleRate
	}
	if sampleRate != s.sampleRate {
		if err := s.resetStreamLocked(sampleRate); err != nil {
			return false, err
		}
	}

	pcm := downmixToMono(pcmData, s.channels)
	if len(pcm) == 0 {
		return false, nil
	}

	s.pending = append(s.pending, pcm...)
	windowSize := sileroWindowSize(s.sampleRate)
	processed := 0
	haveVoice := s.lastVoice
	chunkHasVoice := false
	didInfer := false

	for processed+windowSize <= len(s.pending) {
		probability, err := s.stream.Infer(s.pending[processed : processed+windowSize])
		if err != nil {
			return false, err
		}
		didInfer = true
		haveVoice = s.detector.Evaluate(probability)
		if haveVoice {
			chunkHasVoice = true
		}
		processed += windowSize
	}

	if processed > 0 {
		remaining := copy(s.pending, s.pending[processed:])
		s.pending = s.pending[:remaining]
	}
	if didInfer {
		s.lastVoice = haveVoice
		return chunkHasVoice, nil
	}
	return s.lastVoice, nil
}

func (s *SileroVAD) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	key := s.runtimeKey
	s.stream = nil
	s.runtime = nil
	s.pending = nil
	s.closed = true
	s.mu.Unlock()

	return releaseRuntime(key)
}

func (s *SileroVAD) IsValid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.runtime != nil && s.stream != nil
}

func AcquireVAD(config map[string]interface{}) (VAD, error) {
	return NewSileroVAD(config)
}

func ReleaseVAD(vad VAD) error {
	if vad != nil {
		return vad.Close()
	}
	return nil
}

func (s *SileroVAD) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.stream == nil {
		return nil
	}
	s.pending = nil
	s.lastVoice = false
	if s.detector != nil {
		s.detector.Reset()
	}
	return s.stream.Reset()
}

func (s *SileroVAD) SetThreshold(threshold float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vadThreshold = threshold
	s.detector = hysteresis.NewDetector(s.vadThreshold, s.vadThresholdLow)
	if s.stream != nil {
		s.stream.SetThreshold(threshold)
	}
}

func (s *SileroVAD) resetStreamLocked(sampleRate int) error {
	stream, err := s.runtime.NewStream(speech.StreamConfig{
		SampleRate:           sampleRate,
		Threshold:            s.vadThreshold,
		MinSilenceDurationMs: s.silenceThreshold,
		SpeechPadMs:          s.speechPadMs,
	})
	if err != nil {
		return err
	}
	s.stream = stream
	s.sampleRate = sampleRate
	s.pending = nil
	s.lastVoice = false
	if s.detector != nil {
		s.detector.Reset()
	}
	return nil
}

func acquireRuntime(cfg speech.RuntimeConfig) (runtimeKey, *speech.Runtime, error) {
	key := runtimeKey{
		modelPath:         filepath.Clean(cfg.ModelPath),
		logLevel:          cfg.LogLevel,
		numSessions:       cfg.NumSessions,
		intraOpNumThreads: cfg.IntraOpNumThreads,
		interOpNumThreads: cfg.InterOpNumThreads,
	}

	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	if shared, ok := runtimes[key]; ok {
		shared.refs++
		log.Debugf(
			"Silero VAD共享Runtime复用: model=%s, num_sessions=%d, refs=%d",
			key.modelPath,
			key.numSessions,
			shared.refs,
		)
		return key, shared.runtime, nil
	}

	rt, err := speech.NewRuntime(cfg)
	if err != nil {
		return key, nil, err
	}
	runtimes[key] = &sharedRuntime{runtime: rt, refs: 1}
	log.Debugf(
		"Silero VAD共享Runtime创建: model=%s, num_sessions=%d, intra_threads=%d, inter_threads=%d, refs=1",
		key.modelPath,
		key.numSessions,
		key.intraOpNumThreads,
		key.interOpNumThreads,
	)
	return key, rt, nil
}

func releaseRuntime(key runtimeKey) error {
	runtimeMu.Lock()
	shared, ok := runtimes[key]
	if !ok {
		runtimeMu.Unlock()
		return nil
	}
	shared.refs--
	if shared.refs > 0 {
		runtimeMu.Unlock()
		return nil
	}
	delete(runtimes, key)
	runtimeMu.Unlock()

	return shared.runtime.Destroy()
}

func resolveModelPath(modelPath string) (string, error) {
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" {
		return "", errors.New("缺少模型路径配置")
	}

	candidates := buildModelPathCandidates(modelPath, modelPathSearchRoots()...)
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("模型文件不存在: %s (已尝试: %s)", modelPath, strings.Join(candidates, ", "))
}

func buildModelPathCandidates(modelPath string, roots ...string) []string {
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" {
		return nil
	}

	cleanedPath := filepath.Clean(filepath.FromSlash(modelPath))
	if filepath.IsAbs(cleanedPath) {
		return []string{cleanedPath}
	}

	variants := []string{cleanedPath}
	switch cleanedPath {
	case filepath.Clean(filepath.FromSlash(sourceSileroVADModelPath)):
		variants = append(variants, filepath.Clean(filepath.FromSlash(releaseSileroVADModelPath)))
	case filepath.Clean(filepath.FromSlash(releaseSileroVADModelPath)):
		variants = append(variants, filepath.Clean(filepath.FromSlash(sourceSileroVADModelPath)))
	}

	if len(roots) == 0 {
		roots = []string{""}
	}

	seen := make(map[string]struct{})
	candidates := make([]string, 0, len(roots)*len(variants))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		for _, variant := range variants {
			candidate := variant
			if root != "" {
				candidate = filepath.Join(root, variant)
			}
			candidate = filepath.Clean(candidate)
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func modelPathSearchRoots() []string {
	roots := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		roots = append(roots, cwd)
	}
	if executablePath, err := os.Executable(); err == nil && executablePath != "" {
		roots = append(roots, filepath.Dir(executablePath))
	}
	if len(roots) == 0 {
		roots = append(roots, "")
	}
	return roots
}

func withDefaults(config map[string]interface{}) map[string]interface{} {
	cfg := make(map[string]interface{}, len(defaultVADConfig)+len(config))
	for k, v := range defaultVADConfig {
		cfg[k] = v
	}
	for k, v := range config {
		cfg[k] = v
	}
	return cfg
}

func getString(config map[string]interface{}, key, fallback string) string {
	if value, ok := config[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func getInt(config map[string]interface{}, key string, fallback int) int {
	switch value := config[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case int32:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	}
	return fallback
}

func getFloat32(config map[string]interface{}, key string, fallback float32) float32 {
	switch value := config[key].(type) {
	case float32:
		return value
	case float64:
		return float32(value)
	case int:
		return float32(value)
	case int64:
		return float32(value)
	}
	return fallback
}

func getDurationMs(config map[string]interface{}, msKey, secondsKey string, fallback int) int {
	if _, ok := config[msKey]; ok {
		return getInt(config, msKey, fallback)
	}
	if _, ok := config[secondsKey]; ok {
		return int(getFloat32(config, secondsKey, float32(fallback)/1000) * 1000)
	}
	return fallback
}

func resolveNumSessions(config map[string]interface{}) int {
	defaultNumSessions := runtime.NumCPU()
	numSessions := getInt(config, "num_sessions", getInt(config, "pool_size", defaultNumSessions))
	if numSessions <= 0 {
		return defaultNumSessions
	}
	return numSessions
}

func sileroWindowSize(sampleRate int) int {
	if sampleRate == 8000 {
		return 256
	}
	return 512
}

func downmixToMono(pcm []float32, channels int) []float32 {
	if channels <= 1 {
		return pcm
	}
	frameCount := len(pcm) / channels
	mono := make([]float32, frameCount)
	for frame := 0; frame < frameCount; frame++ {
		var sum float32
		offset := frame * channels
		for channel := 0; channel < channels; channel++ {
			sum += pcm[offset+channel]
		}
		mono[frame] = sum / float32(channels)
	}
	return mono
}
