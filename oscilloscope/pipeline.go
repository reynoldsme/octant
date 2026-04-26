package oscilloscope

import (
	"image"
	"math"
)

// feed is the internal (already-locked) implementation of Feed.
func (s *Scope) feed(samples [][2]float64) *image.RGBA {
	cfg := s.cfg

	if cfg.FreezeImage {
		return s.rgba
	}

	// Step 1 & 2: obtain samples from the signal generator or audio input,
	// then apply axis transforms and gain (controls zoom / signal scale).
	if cfg.SignalGeneratorOn && s.sigGen != nil {
		n := len(samples)
		if n == 0 {
			n = defaultBatchSize
		}
		samples = s.sigGen.generate(n, cfg)
	}
	samples = applyTransforms(samples, cfg)

	if len(samples) == 0 {
		return s.rgba
	}

	// Step 3: Lanczos upsample.
	if !cfg.DisableFilter && s.lanczosKernel != nil {
		samples = upsampleWithKernel(samples, s.lanczosKernel, lanczosA, lanczosSteps)
	}

	// Step 4: Sweep / time-base.
	// Pass the effective sample rate after upsampling so the sweep advances
	// at the correct speed regardless of whether the filter is active.
	if cfg.SweepOn {
		effectiveSR := s.sampleRate
		if !cfg.DisableFilter && s.lanczosKernel != nil {
			effectiveSR *= float64(lanczosSteps)
		}
		samples = s.sweep.apply(samples, cfg, effectiveSR)
	}

	// Step 5: Phosphor fade.
	// fadeAmount ∈ [0.1, 0.4] for persistence ∈ [-1, 1].
	fadeAmount := float32(0.2 * math.Pow(2, -cfg.Persistence))
	keepAmount := 1.0 - fadeAmount
	for i := range s.accumBuf {
		s.accumBuf[i] *= keepAmount
	}

	// Step 6: Draw line segments into the accumulation buffer.
	// sigma scales with display height so the beam width is proportional.
	sigma := math.Max(float64(s.height)/200.0, 1.0)
	// Arc-length-normalized gain: each segment deposits (drawGain * segLen / sigma)
	// so energy per unit arc is constant regardless of trace velocity.
	// This prevents over-saturation at slow-moving trace extremes.
	// Skip segments longer than 30σ — these are discontinuous jumps (e.g. the
	// sweep trigger reset from off-screen back to -1.3) that would get enormous
	// arc-length-scaled gain and flood the entire canvas with energy.
	drawGain := 4.0
	maxSegLen := 30 * sigma
	for i := 1; i < len(samples); i++ {
		x0, y0 := samples[i-1][0], samples[i-1][1]
		x1, y1 := samples[i][0], samples[i][1]
		dx := (x1 - x0) * float64(s.width) * 0.5
		dy := (y1 - y0) * float64(s.height) * 0.5
		segLen := math.Sqrt(dx*dx + dy*dy)
		if segLen < 1e-9 || segLen > maxSegLen {
			continue
		}
		drawSegment(s.accumBuf, s.width, s.height, x0, y0, x1, y1,
			drawGain*segLen/sigma, sigma)
	}

	// Step 7: Multi-scale Gaussian glow.
	s.computeGlow()

	// Step 8: Tonemap + color + gamma.
	return s.toRGBA(cfg)
}

// applyTransforms applies axis swaps, inversions, and gain to audio samples.
// MainGain is an exponent: actual scale = 2^MainGain (0 = unity, 1 = 2×, -1 = 0.5×).
func applyTransforms(samples [][2]float64, cfg Config) [][2]float64 {
	scale := math.Pow(2, cfg.MainGain)
	out := make([][2]float64, len(samples))
	for i, sp := range samples {
		x, y := sp[0]*scale, sp[1]*scale
		if cfg.SwapXY {
			x, y = y, x
		}
		if cfg.InvertX {
			x = -x
		}
		if cfg.InvertY {
			y = -y
		}
		out[i] = [2]float64{x, y}
	}
	return out
}

// computeGlow runs two separable Gaussian blur passes over accumBuf:
// a tight pass (narrow beam body) and a scatter pass (wide diffuse halo).
// Sigmas are chosen so the glow spans multiple terminal character rows at
// typical terminal resolutions (each char row = 4 pixel rows for octant chars).
func (s *Scope) computeGlow() {
	// Tight glow: ~1 pixel row — visible beam body.
	tightSigma := math.Max(float64(s.height)/40.0, 1.0)
	kt := buildGaussianKernel(tightSigma)
	blurH(s.blurTmp, s.accumBuf, s.width, s.height, kt)
	blurV(s.tightBuf, s.blurTmp, s.width, s.height, kt)

	// Scatter glow: wide diffuse phosphor halo. Sigma tuned to match reference
	// glow width (~9% of canvas at 850px → ~7px at 80px height).
	scatterSigma := math.Max(float64(s.height)/7.0, 2.0)
	ks := buildGaussianKernel(scatterSigma)
	blurH(s.blurTmp, s.accumBuf, s.width, s.height, ks)
	blurV(s.scatterBuf, s.blurTmp, s.width, s.height, ks)
}

// toRGBA tone-maps the glow buffers to *image.RGBA, applying CRT phosphor
// color (HSV hue), exposure, and gamma.
func (s *Scope) toRGBA(cfg Config) *image.RGBA {
	// Match reference: brightness = 2^(exposureStops - 2), so default (0 stops)
	// gives 0.25. ExposureStops doubles/halves per stop.
	exposure := math.Pow(2, cfg.ExposureStops-2)

	// Base hue color from reference getColourFromHue (perceptual sqrt blend).
	hr, hg, hb := hueToRGB(cfg.Hue)

	pix := s.rgba.Pix
	for i := range s.accumBuf {
		// Reference outputFragment with screen.g≈0.5 (CRT phosphor layer):
		//   tight weight  = 1.5 * screen.g² = 1.5 * 0.25 = 0.375
		//   scatter weight = 0.4 * (2 + screen.g + 0) = 0.4 * 2.5 = 1.0
		// Background: light = 1.0*0.35 = 0.35 → t≈0.059 → g≈15 (matches ref g=16).
		scatter := float64(s.scatterBuf[i]) + 0.35
		light := float64(s.accumBuf[i]) + 0.375*float64(s.tightBuf[i]) + 1.0*scatter

		// Tone-map: maps [0,∞) → [0,1).
		t := 1.0 - math.Pow(2, -exposure*light)
		if t < 0 {
			t = 0
		}

		// Reference color formula: mix(colour, white, 0.3 + t^6*0.5) * t
		// The 0.3 base bleach gives warmth even at low intensities.
		t2 := t * t * t       // t^3
		t2 = t2 * t2          // t^6
		blend := 0.3 + t2*0.5
		if blend > 1 {
			blend = 1
		}
		cr := (hr*(1-blend) + blend) * t
		cg := (hg*(1-blend) + blend) * t
		cb := (hb*(1-blend) + blend) * t

		off := i * 4
		pix[off+0] = uint8(clampF(cr, 0, 1) * 255)
		pix[off+1] = uint8(clampF(cg, 0, 1) * 255)
		pix[off+2] = uint8(clampF(cb, 0, 1) * 255)
		pix[off+3] = 255
	}

	// Step 9: Grid overlay.
	if cfg.GridMode < 2 {
		drawGrid(s.rgba, cfg)
	}

	return s.rgba
}
