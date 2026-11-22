package hysteresis

type Detector struct {
	High   float32
	Low    float32
	active bool
}

func NewDetector(high, low float32) *Detector {
	if high < 0 {
		high = 0
	}
	if high > 1 {
		high = 1
	}
	if low < 0 || low > high {
		low = high
	}
	return &Detector{High: high, Low: low}
}

func (d *Detector) Evaluate(score float32) bool {
	if d == nil {
		return false
	}
	if score >= d.High {
		d.active = true
		return true
	}
	if score <= d.Low {
		d.active = false
		return false
	}
	return d.active
}

func (d *Detector) Reset() {
	if d == nil {
		return
	}
	d.active = false
}

func (d *Detector) Active() bool {
	if d == nil {
		return false
	}
	return d.active
}
