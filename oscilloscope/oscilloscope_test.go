package oscilloscope

import (
	"math"
	"testing"
)

func TestDefaultConfigValidates(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateClampsRanges(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MainGain = 99
	cfg.Persistence = -5
	cfg.SweepMsDiv = 0
	cfg.AExponent = 10
	cfg.Validate()
	if cfg.MainGain != 4 {
		t.Errorf("MainGain not clamped: %v", cfg.MainGain)
	}
	if cfg.Persistence != -1 {
		t.Errorf("Persistence not clamped: %v", cfg.Persistence)
	}
	if cfg.SweepMsDiv != 0.25 {
		t.Errorf("SweepMsDiv not clamped: %v", cfg.SweepMsDiv)
	}
	if cfg.AExponent != 3 {
		t.Errorf("AExponent not clamped: %v", cfg.AExponent)
	}
}

func TestExpressionParser(t *testing.T) {
	tests := []struct {
		expr string
		env  map[string]float64
		want float64
	}{
		{"2 + 3", nil, 5},
		{"2 * 3 + 1", nil, 7},
		{"(2 + 3) * 4", nil, 20},
		{"-5 + 3", nil, -2},
		{"sin(0)", nil, 0},
		{"cos(0)", nil, 1},
		{"PI", map[string]float64{"PI": math.Pi}, math.Pi},
		{"a + b", map[string]float64{"a": 3, "b": 2}, 5},
		{"sin(2*PI*a*t)*cos(2*PI*b*t)",
			map[string]float64{"PI": math.Pi, "a": 1, "b": 1, "t": 0},
			0}, // sin(0)*cos(0) = 0
	}
	for _, tt := range tests {
		e, err := compile(tt.expr)
		if err != nil {
			t.Errorf("compile(%q): %v", tt.expr, err)
			continue
		}
		env := tt.env
		if env == nil {
			env = map[string]float64{}
		}
		got := e.eval(env)
		if math.Abs(got-tt.want) > 1e-10 {
			t.Errorf("eval(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestExpressionParserErrors(t *testing.T) {
	bad := []string{
		"(",
		"1 +",
		"@",
		"sin(",
	}
	for _, expr := range bad {
		if _, err := compile(expr); err == nil {
			t.Errorf("compile(%q) should have failed", expr)
		}
	}
}

func TestSignalGenerator(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SignalGeneratorOn = true
	g, err := newSignalGenerator(cfg, 44100)
	if err != nil {
		t.Fatal(err)
	}
	samples := g.generate(1024, cfg)
	if len(samples) != 1024 {
		t.Errorf("got %d samples, want 1024", len(samples))
	}
	// At t=0: X = sin(0)*cos(0) = 0, Y = cos(0)*cos(0) = 1
	if math.Abs(samples[0][1]-1.0) > 0.01 {
		t.Errorf("Y[0] = %v, want ~1.0", samples[0][1])
	}
}

func TestLanczosKernel(t *testing.T) {
	kernel := precomputeKernel(lanczosA, lanczosSteps)
	if len(kernel) != lanczosSteps {
		t.Fatalf("kernel phases: got %d, want %d", len(kernel), lanczosSteps)
	}
	// Each phase should sum to ~1 (normalized).
	for p, phase := range kernel {
		sum := 0.0
		for _, w := range phase {
			sum += w
		}
		if math.Abs(sum-1.0) > 1e-6 {
			t.Errorf("phase %d: sum = %v, want 1.0", p, sum)
		}
	}
}

func TestLanczosUpsample(t *testing.T) {
	kernel := precomputeKernel(lanczosA, lanczosSteps)
	// DC input: all samples = (1, -1); upsampled output should stay (1, -1).
	samples := make([][2]float64, 32)
	for i := range samples {
		samples[i] = [2]float64{1, -1}
	}
	out := upsampleWithKernel(samples, kernel, lanczosA, lanczosSteps)
	if len(out) != len(samples)*lanczosSteps {
		t.Fatalf("output length: got %d, want %d", len(out), len(samples)*lanczosSteps)
	}
	// Check interior samples (avoid boundary effects).
	for i := lanczosA * lanczosSteps; i < len(out)-lanczosA*lanczosSteps; i++ {
		if math.Abs(out[i][0]-1.0) > 0.01 {
			t.Errorf("out[%d][0] = %v (DC pass failed)", i, out[i][0])
			break
		}
	}
}

func TestSweepTrigger(t *testing.T) {
	sw := newSweepState()
	cfg := DefaultConfig()
	cfg.SweepOn = true
	cfg.SweepTriggerValue = 0

	// Build a sine wave: should trigger when crossing 0 from below.
	n := 256
	samples := make([][2]float64, n)
	for i := range samples {
		v := math.Sin(2 * math.Pi * float64(i) / float64(n))
		samples[i] = [2]float64{0, v}
	}

	out := sw.apply(samples, cfg, 44100)
	if len(out) != n {
		t.Fatalf("output length mismatch")
	}
	// X should have been reset to -1.3 at some point.
	resetFound := false
	for _, s := range out {
		if s[0] <= -1.2 {
			resetFound = true
			break
		}
	}
	if !resetFound {
		t.Error("sweep never reset to -1.3")
	}
}

func TestScopeRender(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SignalGeneratorOn = true
	cfg.GridMode = 2 // skip grid for simpler test
	scope := New(cfg, 44100)
	scope.Resize(160, 80)

	// Run several frames to build up phosphor accumulation.
	var frame interface{ Bounds() interface{ Dx() int } }
	_ = frame
	for range 5 {
		f := scope.Feed(nil)
		if f == nil {
			t.Fatal("Feed returned nil")
		}
	}

	img := scope.Feed(nil)
	if img.Bounds().Dx() != 160 || img.Bounds().Dy() != 80 {
		t.Errorf("frame size %dx%d, want 160x80", img.Bounds().Dx(), img.Bounds().Dy())
	}

	// After several frames the Lissajous trace should produce non-black pixels.
	bright := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 10 || img.Pix[i+1] > 10 || img.Pix[i+2] > 10 {
			bright++
		}
	}
	if bright == 0 {
		t.Error("all pixels are black after 6 frames with signal generator")
	}
}

func TestHSVToRGB(t *testing.T) {
	tests := []struct {
		h, s, v    float64
		r, g, b    float64
	}{
		{0, 1, 1, 1, 0, 0},    // red
		{120, 1, 1, 0, 1, 0},  // green
		{240, 1, 1, 0, 0, 1},  // blue
		{0, 0, 1, 1, 1, 1},    // white
		{0, 0, 0, 0, 0, 0},    // black
	}
	for _, tt := range tests {
		r, g, b := hsvToRGB(tt.h, tt.s, tt.v)
		if math.Abs(r-tt.r) > 0.001 || math.Abs(g-tt.g) > 0.001 || math.Abs(b-tt.b) > 0.001 {
			t.Errorf("hsvToRGB(%v,%v,%v) = (%v,%v,%v), want (%v,%v,%v)",
				tt.h, tt.s, tt.v, r, g, b, tt.r, tt.g, tt.b)
		}
	}
}
