package oscilloscope

import (
	"fmt"
	"math"
)

// Config holds all oscilloscope display, sweep, and signal-generator parameters.
// Use DefaultConfig to obtain a ready-to-use value.
type Config struct {
	// Display
	MainGain      float64 // -1..4, gain applied to input samples
	ExposureStops float64 // -2..2, brightness in stops (powers of 2)
	Hue           float64 // 0..360, CRT phosphor hue
	Persistence   float64 // -1..1, phosphor afterglow (-1=short, +1=long)
	FreezeImage   bool    // pause display updates
	DisableFilter bool    // skip Lanczos upsampling
	GridMode      int     // 0=black grid, 1=colored grid, 2=no grid
	SwapXY        bool    // swap X and Y axes
	InvertX       bool    // invert X axis
	InvertY       bool    // invert Y axis

	// Sweep / time-base
	SweepOn           bool
	SweepAutoTrigger  bool    // re-trigger automatically when sweep reaches end
	SweepTriggerValue float64 // -1..1, trigger threshold
	SweepMsDiv        float64 // 0.25..4, milliseconds per division

	// Signal generator
	SignalGeneratorOn bool
	XExpression       string  // expression in t, a, b, sin, cos, PI
	YExpression       string  // expression in t, a, b, sin, cos, PI
	AValue            float64 // 0.5..5, 'a' parameter multiplier
	BValue            float64 // 0.5..5, 'b' parameter multiplier
	AExponent         int     // 0..3, 'a' parameter scale exponent (×10^n)
	BExponent         int     // 0..3, 'b' parameter scale exponent (×10^n)
}

// DefaultConfig returns a Config with sensible defaults: green phosphor Lissajous
// pattern with grid, no sweep, Lanczos upsampling enabled.
func DefaultConfig() Config {
	return Config{
		MainGain:      0.0,
		ExposureStops: 0.0,
		Hue:           120.0,
		Persistence:   0.0,
		GridMode:      0,
		SweepOn:       true,
		SweepMsDiv:    4.0,
		AValue:        3.0,
		BValue:        2.0,
		AExponent:     2,
		BExponent:     2,
		XExpression:   "sin(2*PI*a*t)*cos(2*PI*b*t)",
		YExpression:   "cos(2*PI*a*t)*cos(2*PI*b*t)",
	}
}

// Validate clamps all numeric fields to their valid ranges and verifies
// expression syntax. Returns an error if an expression fails to parse.
func (c *Config) Validate() error {
	c.MainGain = clampF(c.MainGain, -1, 4)
	c.ExposureStops = clampF(c.ExposureStops, -4, 4)
	c.Hue = math.Mod(c.Hue, 360)
	if c.Hue < 0 {
		c.Hue += 360
	}
	c.Persistence = clampF(c.Persistence, -1, 1)
	c.SweepTriggerValue = clampF(c.SweepTriggerValue, -1, 1)
	c.SweepMsDiv = clampF(c.SweepMsDiv, 0.25, 32)
	c.AValue = clampF(c.AValue, 0.5, 5)
	c.BValue = clampF(c.BValue, 0.5, 5)
	if c.AExponent < 0 {
		c.AExponent = 0
	}
	if c.AExponent > 3 {
		c.AExponent = 3
	}
	if c.BExponent < 0 {
		c.BExponent = 0
	}
	if c.BExponent > 3 {
		c.BExponent = 3
	}
	if c.XExpression == "" {
		c.XExpression = "sin(2*PI*a*t)*cos(2*PI*b*t)"
	}
	if c.YExpression == "" {
		c.YExpression = "cos(2*PI*a*t)*cos(2*PI*b*t)"
	}
	if _, err := compile(c.XExpression); err != nil {
		return fmt.Errorf("x-expression: %w", err)
	}
	if _, err := compile(c.YExpression); err != nil {
		return fmt.Errorf("y-expression: %w", err)
	}
	return nil
}

func clampF(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

// hueToRGB converts hue [0,360] to an RGB colour using the reference
// oscilloscope's perceptual sqrt-interpolated palette. Each 120° sector
// blends between two primaries with a square-root transition.
func hueToRGB(hue float64) (r, g, b float64) {
	alpha := math.Mod(hue/120.0, 1.0)
	start := math.Sqrt(1.0 - alpha)
	end := math.Sqrt(alpha)
	switch {
	case hue < 120:
		return start, end, 0
	case hue < 240:
		return 0, start, end
	default:
		return end, 0, start
	}
}

// hsvToRGB converts HSV (h in [0,360], s and v in [0,1]) to linear RGB [0,1].
func hsvToRGB(h, s, v float64) (r, g, b float64) {
	hi := int(h/60) % 6
	f := h/60 - math.Floor(h/60)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	switch hi {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	default:
		return v, p, q
	}
}
