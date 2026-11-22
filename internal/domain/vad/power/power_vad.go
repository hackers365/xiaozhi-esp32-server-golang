package power

import "math"

const (
	rmsThreshold  float32 = 0.003
	peakThreshold float32 = 0.015
)

func IsSilent(pcm []float32) bool {
	if len(pcm) == 0 {
		return true
	}

	var sumSquares float64
	var peak float32
	for _, sample := range pcm {
		abs := sample
		if abs < 0 {
			abs = -abs
		}
		if abs > peak {
			peak = abs
		}
		sumSquares += float64(sample * sample)
	}

	rms := float32(math.Sqrt(sumSquares / float64(len(pcm))))
	return rms < rmsThreshold && peak < peakThreshold
}
