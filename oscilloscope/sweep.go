package oscilloscope


// sweepState holds persistent trigger/sweep state across Feed calls.
type sweepState struct {
	sweepX      float64 // current normalized X position, starts at -1.3
	belowThresh bool    // hysteresis: true once Y has dropped below (thresh - hysteresis)
}

func newSweepState() *sweepState {
	return &sweepState{sweepX: -1.3}
}

const sweepHysteresis = 0.05

// apply replaces the X channel with a time-base derived from sweepX,
// advancing each sample and resetting on a rising edge at the trigger threshold.
func (sw *sweepState) apply(samples [][2]float64, cfg Config, sampleRate float64) [][2]float64 {
	// Reference: sweepPosition += 2/sampleRate/sweepMinTime
	// sweepMinTime = sweepMsDiv * 10 / 1000
	sweepMinTime := cfg.SweepMsDiv * 10.0 / 1000.0
	dx := 2.0 / sampleRate / sweepMinTime

	trigThresh := cfg.SweepTriggerValue
	out := make([][2]float64, len(samples))
	for i, s := range samples {
		y := s[1]

		// X is the sweep time position; gain only affects Y amplitude
		// (applied upstream in applyTransforms), not the time axis.
		out[i][0] = sw.sweepX
		out[i][1] = y
		sw.sweepX += dx

		// Auto-trigger: if the sweep has run past the right edge without a
		// trigger event, restart it so the display keeps updating.
		if cfg.SweepAutoTrigger && sw.sweepX > 1.3 {
			sw.sweepX = -1.3
			sw.belowThresh = false
		}

		// Allow retriggering once Y has been below the hysteresis band.
		if !sw.belowThresh && y < trigThresh-sweepHysteresis {
			sw.belowThresh = true
		}
		// Rising edge: only retrigger once sweep has completed a full pass
		// (sweepX > 1.1). Without this guard, complex signals trigger
		// constantly and sweep looks identical to XY mode.
		if sw.sweepX > 1.1 && sw.belowThresh && y >= trigThresh {
			sw.sweepX = -1.3
			sw.belowThresh = false
		}
	}
	return out
}
