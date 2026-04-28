package oscilloscope

import "math"

const (
	lanczosA     = 8 // window size in input samples
	lanczosSteps = 6 // upsampling factor
)

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	px := math.Pi * x
	return math.Sin(px) / px
}

// precomputeKernel builds a polyphase Lanczos-a filter bank.
// Returns kernel[phase][tap] for phase in [0, steps) and tap in [0, 2*a).
//
// For output sample at continuous position x = i0 + phase/steps:
//   output = sum_{t=0}^{2a-1} samples[i0 - a + 1 + t] * kernel[phase][t]
func precomputeKernel(a, steps int) [][]float64 {
	kernel := make([][]float64, steps)
	for phase := range steps {
		kernel[phase] = make([]float64, 2*a)
		sum := 0.0
		for tap := range 2 * a {
			// fractional offset from center input sample (i0) to this tap
			dx := float64(tap-a+1) - float64(phase)/float64(steps)
			var w float64
			if dx == 0 {
				w = 1
			} else if math.Abs(dx) < float64(a) {
				w = sinc(dx) * math.Pow(sinc(dx/float64(a)), 1.5)
			}
			kernel[phase][tap] = w
			sum += w
		}
		if sum != 0 {
			for tap := range kernel[phase] {
				kernel[phase][tap] /= sum
			}
		}
	}
	return kernel
}

// upsampleInto applies the polyphase Lanczos filter into a pre-allocated out
// slice. out must have length len(samples)*steps. Using explicit i0/phase
// counters avoids a division and modulo per output sample.
func upsampleInto(out, samples [][2]float64, kernel [][]float64, a, steps int) {
	nIn := len(samples)
	i0, phase := 0, 0
	for oi := range out {
		taps := kernel[phase]
		var sx, sy float64
		for t, w := range taps {
			si := i0 - a + 1 + t
			if si < 0 {
				si = 0
			} else if si >= nIn {
				si = nIn - 1
			}
			sx += samples[si][0] * w
			sy += samples[si][1] * w
		}
		out[oi][0] = sx
		out[oi][1] = sy
		phase++
		if phase == steps {
			phase = 0
			i0++
		}
	}
}

// upsampleWithKernel applies the polyphase Lanczos filter, returning
// steps × len(samples) output samples.
func upsampleWithKernel(samples [][2]float64, kernel [][]float64, a, steps int) [][2]float64 {
	out := make([][2]float64, len(samples)*steps)
	upsampleInto(out, samples, kernel, a, steps)
	return out
}
