package emotion

import (
	"context"
)

type Emotion int

const (
	EmotionAngry       Emotion = 0  // 愤怒平息
	EmotionComfort     Emotion = 1  // 安慰
	EmotionHappy       Emotion = 2  // 高兴
	EmotionNeutral     Emotion = 3  // 中性/默认
	EmotionSerious     Emotion = 4  // 严肃
	EmotionExcited     Emotion = 5  // 激动
	EmotionApologetic  Emotion = 6  // 道歉
	EmotionEncouraging Emotion = 7  // 鼓励
	EmotionCurious     Emotion = 8  // 好奇
	EmotionWarm        Emotion = 9  // 温暖
	EmotionSad         Emotion = 10 // 悲伤/沮丧
	EmotionAmazed      Emotion = 11 // 惊叹
	EmotionDeepShout   Emotion = 12 // 深沉大声呐喊
	EmotionTrembling   Emotion = 13 // 颤抖
	EmotionSarcastic   Emotion = 14 // 讽刺
	EmotionDracula     Emotion = 15 // 德古拉风格
	EmotionBored       Emotion = 16 // 无聊
	EmotionTired       Emotion = 17 // 疲惫
	EmotionScornful    Emotion = 18 // 轻蔑
	EmotionShouting    Emotion = 19 // 大喊
	EmotionASMR        Emotion = 20 // ASMR 轻柔耳语
	EmotionPanicked    Emotion = 21 // 恐慌
	EmotionMischievous Emotion = 22 // 调皮
	EmotionWhisper     Emotion = 23 // 耳语
	EmotionReluctant   Emotion = 24 // 不情愿
	EmotionCrying      Emotion = 25 // 哭泣
	EmotionVerySlow    Emotion = 26 // 非常缓慢
	EmotionVeryFast    Emotion = 27 // 非常快速
)

type EmotionConfig struct {
	Emotion    Emotion `json:"emotion"`
	Speed      float64 `json:"speed"`       // 语速倍率
	VoiceStyle string  `json:"voice_style"` // 音色/风格名称
	PitchShift float64 `json:"pitch_shift"` // 音调偏移
}

var defaultConfigs = map[Emotion]EmotionConfig{
	EmotionAngry:       {Emotion: EmotionAngry, Speed: 0.90, VoiceStyle: "calm", PitchShift: -0.10},
	EmotionComfort:     {Emotion: EmotionComfort, Speed: 0.85, VoiceStyle: "gentle", PitchShift: -0.05},
	EmotionHappy:       {Emotion: EmotionHappy, Speed: 1.10, VoiceStyle: "cheerful", PitchShift: 0.05},
	EmotionNeutral:     {Emotion: EmotionNeutral, Speed: 1.00, VoiceStyle: "default", PitchShift: 0.00},
	EmotionSerious:     {Emotion: EmotionSerious, Speed: 0.95, VoiceStyle: "serious", PitchShift: -0.05},
	EmotionExcited:     {Emotion: EmotionExcited, Speed: 1.15, VoiceStyle: "excited", PitchShift: 0.10},
	EmotionApologetic:  {Emotion: EmotionApologetic, Speed: 0.90, VoiceStyle: "gentle", PitchShift: -0.05},
	EmotionEncouraging: {Emotion: EmotionEncouraging, Speed: 1.05, VoiceStyle: "cheerful", PitchShift: 0.05},
	EmotionCurious:     {Emotion: EmotionCurious, Speed: 1.00, VoiceStyle: "default", PitchShift: 0.00},
	EmotionWarm:        {Emotion: EmotionWarm, Speed: 0.95, VoiceStyle: "gentle", PitchShift: 0.02},
	EmotionSad:         {Emotion: EmotionSad, Speed: 0.85, VoiceStyle: "sad_crying", PitchShift: -0.08},
	EmotionAmazed:      {Emotion: EmotionAmazed, Speed: 1.08, VoiceStyle: "amazed", PitchShift: 0.08},
	EmotionDeepShout:   {Emotion: EmotionDeepShout, Speed: 1.00, VoiceStyle: "deep_shouting", PitchShift: -0.12},
	EmotionTrembling:   {Emotion: EmotionTrembling, Speed: 0.85, VoiceStyle: "trembling", PitchShift: -0.05},
	EmotionSarcastic:   {Emotion: EmotionSarcastic, Speed: 0.98, VoiceStyle: "sarcastic", PitchShift: 0.02},
	EmotionDracula:     {Emotion: EmotionDracula, Speed: 0.85, VoiceStyle: "dracula", PitchShift: -0.15},
	EmotionBored:       {Emotion: EmotionBored, Speed: 0.85, VoiceStyle: "bored", PitchShift: -0.08},
	EmotionTired:       {Emotion: EmotionTired, Speed: 0.75, VoiceStyle: "tired", PitchShift: -0.10},
	EmotionScornful:    {Emotion: EmotionScornful, Speed: 0.95, VoiceStyle: "scornful", PitchShift: 0.02},
	EmotionShouting:    {Emotion: EmotionShouting, Speed: 1.10, VoiceStyle: "shouting", PitchShift: 0.10},
	EmotionASMR:        {Emotion: EmotionASMR, Speed: 0.75, VoiceStyle: "asmr", PitchShift: -0.04},
	EmotionPanicked:    {Emotion: EmotionPanicked, Speed: 1.20, VoiceStyle: "panicked", PitchShift: 0.10},
	EmotionMischievous: {Emotion: EmotionMischievous, Speed: 1.05, VoiceStyle: "mischievous", PitchShift: 0.05},
	EmotionWhisper:     {Emotion: EmotionWhisper, Speed: 0.80, VoiceStyle: "whisper", PitchShift: -0.06},
	EmotionReluctant:   {Emotion: EmotionReluctant, Speed: 0.85, VoiceStyle: "reluctant", PitchShift: -0.04},
	EmotionCrying:      {Emotion: EmotionCrying, Speed: 0.75, VoiceStyle: "crying", PitchShift: -0.10},
	EmotionVerySlow:    {Emotion: EmotionVerySlow, Speed: 0.65, VoiceStyle: "very_slow", PitchShift: -0.02},
	EmotionVeryFast:    {Emotion: EmotionVeryFast, Speed: 1.35, VoiceStyle: "very_fast", PitchShift: 0.04},
}

func GetEmotionConfig(e Emotion) EmotionConfig {
	if cfg, ok := defaultConfigs[e]; ok {
		return cfg
	}
	return defaultConfigs[EmotionNeutral]
}

type emotionConfigKey struct{}

func WithEmotionConfig(ctx context.Context, cfg EmotionConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, emotionConfigKey{}, cfg)
}

func FromContext(ctx context.Context) (EmotionConfig, bool) {
	if ctx == nil {
		return EmotionConfig{}, false
	}
	cfg, ok := ctx.Value(emotionConfigKey{}).(EmotionConfig)
	return cfg, ok
}

// DeviceEmotion 表示设备端协议规定的标准情绪字符串类型
type DeviceEmotion string

const (
	DeviceEmotionNeutral    DeviceEmotion = "neutral"
	DeviceEmotionHappy      DeviceEmotion = "happy"
	DeviceEmotionLaughing   DeviceEmotion = "laughing"
	DeviceEmotionSad        DeviceEmotion = "sad"
	DeviceEmotionAngry      DeviceEmotion = "angry"
	DeviceEmotionThinking   DeviceEmotion = "thinking"
	DeviceEmotionListening  DeviceEmotion = "listening"
	DeviceEmotionSpeaking   DeviceEmotion = "speaking"
	DeviceEmotionSleepy     DeviceEmotion = "sleepy"
	DeviceEmotionWinking    DeviceEmotion = "winking"
	DeviceEmotionSurprised  DeviceEmotion = "surprised"
	DeviceEmotionConfused   DeviceEmotion = "confused"
	DeviceEmotionShocked    DeviceEmotion = "shocked"
	DeviceEmotionBlushing   DeviceEmotion = "blushing"
	DeviceEmotionCool       DeviceEmotion = "cool"
	DeviceEmotionLoving     DeviceEmotion = "loving"
	DeviceEmotionFearful    DeviceEmotion = "fearful"
	DeviceEmotionCrying     DeviceEmotion = "crying"
	DeviceEmotionRelaxed    DeviceEmotion = "relaxed"
	DeviceEmotionRobot2     DeviceEmotion = "robot_2"
	DeviceEmotionCloudSlash DeviceEmotion = "cloud_slash"
)

type DeviceEmotionPair struct {
	Emotion DeviceEmotion
	Emoji   string
}

var deviceEmotionMap = map[Emotion]DeviceEmotionPair{
	EmotionAngry:       {DeviceEmotionAngry, "😡"},
	EmotionComfort:     {DeviceEmotionLoving, "😍"},
	EmotionHappy:       {DeviceEmotionHappy, "😀"},
	EmotionNeutral:     {DeviceEmotionNeutral, "😐"},
	EmotionSerious:     {DeviceEmotionThinking, "🤔"},
	EmotionExcited:     {DeviceEmotionLaughing, "😆"},
	EmotionApologetic:  {DeviceEmotionBlushing, "😳"},
	EmotionEncouraging: {DeviceEmotionHappy, "😀"},
	EmotionCurious:     {DeviceEmotionConfused, "😕"},
	EmotionWarm:        {DeviceEmotionLoving, "😍"},
	EmotionSad:         {DeviceEmotionSad, "😢"},
	EmotionAmazed:      {DeviceEmotionSurprised, "😮"},
	EmotionDeepShout:   {DeviceEmotionShocked, "😱"},
	EmotionTrembling:   {DeviceEmotionFearful, "😨"},
	EmotionSarcastic:   {DeviceEmotionCool, "😎"},
	EmotionDracula:     {DeviceEmotionCool, "😎"},
	EmotionBored:       {DeviceEmotionSleepy, "😴"},
	EmotionTired:       {DeviceEmotionSleepy, "😴"},
	EmotionScornful:    {DeviceEmotionCool, "😎"},
	EmotionShouting:    {DeviceEmotionShocked, "😱"},
	EmotionASMR:        {DeviceEmotionRelaxed, "😌"},
	EmotionPanicked:    {DeviceEmotionFearful, "😨"},
	EmotionMischievous: {DeviceEmotionWinking, "😉"},
	EmotionWhisper:     {DeviceEmotionRelaxed, "😌"},
	EmotionReluctant:   {DeviceEmotionConfused, "😕"},
	EmotionCrying:      {DeviceEmotionCrying, "😭"},
	EmotionVerySlow:    {DeviceEmotionNeutral, "😐"},
	EmotionVeryFast:    {DeviceEmotionLaughing, "😆"},
}

// ToDeviceEmotion 将服务端内部情绪枚举映射为设备端标准情绪字符串与 Emoji
func ToDeviceEmotion(e Emotion) (DeviceEmotion, string) {
	if pair, ok := deviceEmotionMap[e]; ok {
		return pair.Emotion, pair.Emoji
	}
	return DeviceEmotionNeutral, "😐"
}
