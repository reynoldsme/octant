// Package oscilloscope implements a real-time oscilloscope rendering engine
// translated from https://dood.al/oscilloscope/.
//
// The Scope type processes stereo audio samples and renders them to *image.RGBA
// frames using a CRT phosphor simulation: Gaussian line drawing, multi-scale
// glow, phosphor persistence, HSV color, and an optional CRT graticule.
//
// Typical usage:
//
//	scope := oscilloscope.New(oscilloscope.DefaultConfig(), 44100)
//	scope.Resize(width, height)
//	for {
//	    frame := scope.Feed(audioSamples) // [][2]float64, left=X right=Y
//	    // render frame to terminal or sixel output
//	}
package oscilloscope

import (
	"image"
	"sync"
)

const defaultBatchSize = 1024

// Scope is the stateful oscilloscope engine. Create with New, resize with
// Resize before the first Feed, then call Feed each render tick.
//
// All public methods are safe to call from multiple goroutines.
type Scope struct {
	mu         sync.Mutex
	cfg        Config
	sampleRate float64
	width      int
	height     int

	// accumBuf holds persistent phosphor state in linear light.
	// Values exceed 1.0 where multiple segments overlap; they are
	// tone-mapped to [0,1] only at the final toRGBA step.
	accumBuf []float32

	// Scratch buffers for the two glow passes; same size as accumBuf.
	blurTmp    []float32
	tightBuf   []float32
	scatterBuf []float32

	// rgba is reused across Feed calls to avoid per-frame allocation.
	rgba *image.RGBA

	// Sweep trigger state, reset on Resize.
	sweep *sweepState

	// Signal generator, created lazily when SignalGeneratorOn is set.
	sigGen *signalGenerator

	// Precomputed Lanczos polyphase filter kernel; nil when DisableFilter.
	lanczosKernel [][]float64

	// Reusable output buffer for Lanczos upsampling; grown as needed.
	upsampleBuf [][2]float64

	// Track previous expression strings to detect changes in SetConfig.
	prevXExpr string
	prevYExpr string
}

// New creates a Scope with the given config and audio sample rate (Hz).
// Call Resize before the first Feed.
func New(cfg Config, sampleRate float64) *Scope {
	s := &Scope{
		cfg:        cfg,
		sampleRate: sampleRate,
		sweep:      newSweepState(),
		prevXExpr:  cfg.XExpression,
		prevYExpr:  cfg.YExpression,
	}
	if cfg.SignalGeneratorOn {
		s.sigGen, _ = newSignalGenerator(cfg, sampleRate)
	}
	s.rebuildLanczos()
	return s
}

// Resize sets the output pixel dimensions. Call this whenever the terminal
// window changes size. For octant output: width = cols*2, height = rows*4.
func (s *Scope) Resize(width, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if width == s.width && height == s.height {
		return
	}
	s.width = width
	s.height = height
	n := width * height
	s.accumBuf = make([]float32, n)
	s.blurTmp = make([]float32, n)
	s.tightBuf = make([]float32, n)
	s.scatterBuf = make([]float32, n)
	s.rgba = image.NewRGBA(image.Rect(0, 0, width, height))
	s.sweep = newSweepState()
}

// SetConfig updates the oscilloscope parameters. Safe to call concurrently
// with Feed (e.g. from a keyboard handler goroutine).
func (s *Scope) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filterChanged := cfg.DisableFilter != s.cfg.DisableFilter
	exprChanged := cfg.XExpression != s.prevXExpr || cfg.YExpression != s.prevYExpr
	sigGenEnabled := cfg.SignalGeneratorOn && !s.cfg.SignalGeneratorOn

	s.cfg = cfg

	if filterChanged {
		s.rebuildLanczos()
	}
	if s.sigGen != nil && exprChanged {
		s.sigGen.setExprs(cfg) //nolint:errcheck — invalid expressions silently no-op
	}
	if sigGenEnabled && s.sigGen == nil {
		s.sigGen, _ = newSignalGenerator(cfg, s.sampleRate)
	}
	s.prevXExpr = cfg.XExpression
	s.prevYExpr = cfg.YExpression
}

// Feed processes audio samples and returns a rendered frame.
// samples is stereo: [i][0] = left (X axis), [i][1] = right (Y axis).
// Pass nil or an empty slice when using the signal generator.
// Returns nil if Resize has not been called yet.
func (s *Scope) Feed(samples [][2]float64) *image.RGBA {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.width == 0 || s.height == 0 {
		return nil
	}
	return s.feed(samples)
}

func (s *Scope) rebuildLanczos() {
	if s.cfg.DisableFilter {
		s.lanczosKernel = nil
	} else {
		s.lanczosKernel = precomputeKernel(lanczosA, lanczosSteps)
	}
}
