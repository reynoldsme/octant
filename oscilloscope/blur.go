package oscilloscope

import "math"

// buildGaussianKernel returns a normalized 1D Gaussian kernel sized to cover
// ±3σ so that at least 99.7% of the distribution is captured.
func buildGaussianKernel(sigma float64) []float64 {
	if sigma < 0.5 {
		return []float64{1.0}
	}
	radius := int(math.Ceil(3.0 * sigma))
	size := 2*radius + 1
	k := make([]float64, size)
	sum := 0.0
	for i := range k {
		x := float64(i - radius)
		k[i] = math.Exp(-x * x / (2.0 * sigma * sigma))
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

// blurH applies a horizontal Gaussian pass: src → dst (same size).
// Clamps at boundaries.
func blurH(dst, src []float32, width, height int, k []float64) {
	radius := len(k) / 2
	for y := range height {
		srcRow := src[y*width:]
		dstRow := dst[y*width:]
		for x := range width {
			var v float64
			for ki, kv := range k {
				sx := x + ki - radius
				if sx < 0 {
					sx = 0
				} else if sx >= width {
					sx = width - 1
				}
				v += float64(srcRow[sx]) * kv
			}
			dstRow[x] = float32(v)
		}
	}
}

// blurV applies a vertical Gaussian pass: src → dst (same size).
// Clamps at boundaries.
func blurV(dst, src []float32, width, height int, k []float64) {
	radius := len(k) / 2
	for y := range height {
		dstRow := dst[y*width:]
		for x := range width {
			var v float64
			for ki, kv := range k {
				sy := y + ki - radius
				if sy < 0 {
					sy = 0
				} else if sy >= height {
					sy = height - 1
				}
				v += float64(src[sy*width+x]) * kv
			}
			dstRow[x] = float32(v)
		}
	}
}
