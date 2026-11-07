package chat

import "testing"

func TestShouldSkipSileroByPowerOnlyBeforeVoiceStarts(t *testing.T) {
	if !shouldSkipSileroByPower(true, false, []float32{0, 0, 0.001, -0.001}) {
		t.Fatal("silent Silero frame before voice starts should be skipped")
	}
	if shouldSkipSileroByPower(false, false, []float32{0, 0, 0.001, -0.001}) {
		t.Fatal("non-Silero VAD should not use power skip")
	}
	if shouldSkipSileroByPower(true, true, []float32{0, 0, 0.001, -0.001}) {
		t.Fatal("Silero frame after voice starts should not be skipped")
	}
	if shouldSkipSileroByPower(true, false, []float32{0.02, 0, 0, 0}) {
		t.Fatal("audible Silero frame should not be skipped")
	}
}
