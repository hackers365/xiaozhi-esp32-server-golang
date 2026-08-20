package notify

import (
	"strings"
	"unicode/utf8"
	"xiaozhi-esp32-server-golang/internal/data/msg"
)

// EstimateSubtitles 根据文本和语速智能切分句子并估算每个分句的 start_ms
func EstimateSubtitles(text string, speed float64) []msg.NotifySubtitle {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	if speed <= 0 {
		speed = 1.0
	}

	// 基础每字符耗时 (毫秒)，标准中文/英文平均约 220ms / 字
	const baseMsPerChar = 220.0
	msPerChar := baseMsPerChar / speed

	// 主要句末标点 (强制切句)
	isMajorPunctuation := func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', '\n', '\r', '.', '!', '?', ';':
			return true
		default:
			return false
		}
	}

	// 次要标点 (逗号/顿号等，用于长句切分)
	isMinorPunctuation := func(r rune) bool {
		switch r {
		case '，', '、', ',':
			return true
		default:
			return false
		}
	}

	var rawSentences []string
	var current strings.Builder

	for _, r := range trimmed {
		current.WriteRune(r)
		if isMajorPunctuation(r) {
			s := strings.TrimSpace(current.String())
			if s != "" {
				rawSentences = append(rawSentences, s)
			}
			current.Reset()
		}
	}

	if current.Len() > 0 {
		s := strings.TrimSpace(current.String())
		if s != "" {
			rawSentences = append(rawSentences, s)
		}
	}

	// 对于特别长的句子（> 25 字符），进一步按逗号拆分
	var sentences []string
	for _, s := range rawSentences {
		if utf8.RuneCountInString(s) > 25 {
			var part strings.Builder
			for _, r := range s {
				part.WriteRune(r)
				if isMinorPunctuation(r) {
					sub := strings.TrimSpace(part.String())
					if sub != "" {
						sentences = append(sentences, sub)
					}
					part.Reset()
				}
			}
			if part.Len() > 0 {
				sub := strings.TrimSpace(part.String())
				if sub != "" {
					sentences = append(sentences, sub)
				}
			}
		} else {
			sentences = append(sentences, s)
		}
	}

	if len(sentences) == 0 {
		return []msg.NotifySubtitle{
			{
				StartMs: 0,
				Text:    trimmed,
			},
		}
	}

	subtitles := make([]msg.NotifySubtitle, 0, len(sentences))
	var currentMs float64 = 0

	for i, sentence := range sentences {
		subtitles = append(subtitles, msg.NotifySubtitle{
			StartMs: uint32(currentMs),
			Text:    sentence,
		})

		// 计算当前句子的时长
		charCount := utf8.RuneCountInString(sentence)
		sentenceDuration := float64(charCount) * msPerChar
		if sentenceDuration < 500 {
			sentenceDuration = 500 // 最短 500ms
		}

		// 累加到下一句的开始时间
		if i < len(sentences)-1 {
			currentMs += sentenceDuration
		}
	}

	return subtitles
}
