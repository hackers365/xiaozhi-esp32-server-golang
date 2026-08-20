package notify

import (
	"testing"
)

func TestEstimateSubtitles(t *testing.T) {
	text := "主人，您的衣物已经洗好了。请及时取出晾晒！"
	subtitles := EstimateSubtitles(text, 1.0)

	if len(subtitles) != 2 {
		t.Fatalf("expected 2 subtitles, got %d", len(subtitles))
	}

	if subtitles[0].StartMs != 0 {
		t.Fatalf("expected first subtitle start_ms 0, got %d", subtitles[0].StartMs)
	}

	if subtitles[0].Text != "主人，您的衣物已经洗好了。" {
		t.Fatalf("unexpected text: %s", subtitles[0].Text)
	}

	if subtitles[1].StartMs <= subtitles[0].StartMs {
		t.Fatalf("expected second subtitle start_ms > first start_ms, got %d", subtitles[1].StartMs)
	}

	if subtitles[1].Text != "请及时取出晾晒！" {
		t.Fatalf("unexpected text: %s", subtitles[1].Text)
	}
}

func TestEstimateSubtitlesEmpty(t *testing.T) {
	subtitles := EstimateSubtitles("", 1.0)
	if len(subtitles) != 0 {
		t.Fatalf("expected 0 subtitles for empty text, got %d", len(subtitles))
	}
}

func TestEstimateSubtitlesNoPunctuation(t *testing.T) {
	text := "没有标点的一句话"
	subtitles := EstimateSubtitles(text, 1.0)
	if len(subtitles) != 1 {
		t.Fatalf("expected 1 subtitle, got %d", len(subtitles))
	}
	if subtitles[0].StartMs != 0 || subtitles[0].Text != text {
		t.Fatalf("unexpected subtitle: %+v", subtitles[0])
	}
}
