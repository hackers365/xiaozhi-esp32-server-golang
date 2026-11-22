package hooks

import "testing"

func TestShouldDropASRNoiseTextDropsPunctuationOnly(t *testing.T) {
	cfg := DefaultASRNoiseFilterConfig()
	cfg.Enabled = true

	drop, reason := ShouldDropASRNoiseText("。。。？！", 0, cfg)

	if !drop {
		t.Fatalf("ShouldDropASRNoiseText() drop = false, want true")
	}
	if reason != "punctuation_only" {
		t.Fatalf("ShouldDropASRNoiseText() reason = %q, want punctuation_only", reason)
	}
}

func TestShouldDropASRNoiseTextDropsConfiguredPhrasesAfterNormalization(t *testing.T) {
	cfg := DefaultASRNoiseFilterConfig()
	cfg.Enabled = true

	cases := []string{
		"嗯。",
		"啊",
		"标识符号",
		"标点符号。",
	}

	for _, text := range cases {
		drop, reason := ShouldDropASRNoiseText(text, 120, cfg)
		if !drop {
			t.Fatalf("ShouldDropASRNoiseText(%q) drop = false, want true", text)
		}
		if reason != "drop_phrase" {
			t.Fatalf("ShouldDropASRNoiseText(%q) reason = %q, want drop_phrase", text, reason)
		}
	}
}

func TestShouldDropASRNoiseTextKeepsConfiguredShortCommands(t *testing.T) {
	cfg := DefaultASRNoiseFilterConfig()
	cfg.Enabled = true

	for _, text := range []string{"好。", "停", "开", "关"} {
		drop, reason := ShouldDropASRNoiseText(text, 80, cfg)
		if drop {
			t.Fatalf("ShouldDropASRNoiseText(%q) drop = true reason=%q, want false", text, reason)
		}
	}
}

func TestShouldDropASRNoiseTextKeepsNoiseWhenDisabled(t *testing.T) {
	cfg := DefaultASRNoiseFilterConfig()
	cfg.Enabled = false

	drop, reason := ShouldDropASRNoiseText("嗯。", 80, cfg)

	if drop {
		t.Fatalf("ShouldDropASRNoiseText() drop = true reason=%q, want false", reason)
	}
	if reason != "disabled" {
		t.Fatalf("ShouldDropASRNoiseText() reason = %q, want disabled", reason)
	}
}
