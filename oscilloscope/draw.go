package oscilloscope

import "math"

// expLUT is a precomputed table of exp(-x) for x ∈ [0, 4.5].
// drawSegment's Gaussian argument is dist²·inv2σ² ∈ [0, 4.5] (the 3σ cutoff),
// so a 512-entry table gives ~0.9% maximum error — imperceptible for a glow.
const (
	expTableSize  = 512
	expTableScale = float64(expTableSize-1) / 4.5 // maps [0, 4.5] → [0, 511]
)

var expLUT [expTableSize]float32

func init() {
	for i := range expLUT {
		expLUT[i] = float32(math.Exp(-float64(i) / expTableScale))
	}
}

// drawSegment rasterizes a line segment from (x0,y0) to (x1,y1) into buf.
//
// Coordinates are in normalized space [-1, 1]×[-1, 1]; the buffer is
// width×height pixels. Each pixel within ~3σ of the segment receives an
// additive Gaussian intensity proportional to its perpendicular distance.
func drawSegment(buf []float32, width, height int, x0, y0, x1, y1, gain, sigma float64) {
	// Map normalized coords to pixel space.
	// X: [-1,1] → [0, width)
	// Y: [-1,1] → [height-1, 0) (Y axis inverted so +1 is top)
	hw := float64(width) * 0.5
	hh := float64(height) * 0.5
	px0 := (x0 + 1.0) * hw
	py0 := (1.0 - y0) * hh
	px1 := (x1 + 1.0) * hw
	py1 := (1.0 - y1) * hh

	dx := px1 - px0
	dy := py1 - py0
	lenSq := dx*dx + dy*dy

	inflate := 3.0 * sigma
	minX := int(math.Floor(math.Min(px0, px1) - inflate))
	maxX := int(math.Ceil(math.Max(px0, px1) + inflate))
	minY := int(math.Floor(math.Min(py0, py1) - inflate))
	maxY := int(math.Ceil(math.Max(py0, py1) + inflate))

	if minX < 0 {
		minX = 0
	}
	if maxX >= width {
		maxX = width - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= height {
		maxY = height - 1
	}

	inv2SigSq := 1.0 / (2.0 * sigma * sigma)
	inflateSq := inflate * inflate
	gainF32 := float32(gain)

	for py := minY; py <= maxY; py++ {
		row := buf[py*width:]
		cy := float64(py) + 0.5
		for px := minX; px <= maxX; px++ {
			cx := float64(px) + 0.5
			var dist2 float64
			if lenSq < 1e-10 {
				ddx, ddy := cx-px0, cy-py0
				dist2 = ddx*ddx + ddy*ddy
			} else {
				t := ((cx-px0)*dx + (cy-py0)*dy) / lenSq
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				ex := px0 + t*dx - cx
				ey := py0 + t*dy - cy
				dist2 = ex*ex + ey*ey
			}
			if dist2 > inflateSq {
				continue
			}
			xi := int(dist2 * inv2SigSq * expTableScale)
			if xi >= expTableSize {
				xi = expTableSize - 1
			}
			row[px] += gainF32 * expLUT[xi]
		}
	}
}
