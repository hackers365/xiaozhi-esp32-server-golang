package power

import "testing"

func TestIsSilentDropsZeroOrVeryLowPower(t *testing.T) {
	if !IsSilent(nil) {
		t.Fatal("nil audio should be silent")
	}
	if !IsSilent([]float32{0, 0, 0, 0}) {
		t.Fatal("zero audio should be silent")
	}
	if !IsSilent([]float32{0.001, -0.001, 0.001, -0.001}) {
		t.Fatal("very low power audio should be silent")
	}
}

func TestIsSilentKeepsRMSAboveThreshold(t *testing.T) {
	pcm := []float32{0.004, -0.004, 0.004, -0.004}

	if IsSilent(pcm) {
		t.Fatal("audio with RMS above threshold should not be silent")
	}
}

func TestIsSilentKeepsPeakAboveThreshold(t *testing.T) {
	pcm := []float32{0, 0, 0.02, 0}

	if IsSilent(pcm) {
		t.Fatal("audio with peak above threshold should not be silent")
	}
}
