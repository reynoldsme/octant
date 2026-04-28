package oscilloscope

import "math"

// gaussBoxRadii returns 3 box-blur radii whose sequential convolution
// approximates a Gaussian with the given sigma (Welford 2006).
// Using 3 box passes instead of a direct Gaussian kernel makes scatter-glow
// O(width×height) regardless of sigma, rather than O(width×height×kernelSize).
func gaussBoxRadii(sigma float64) [3]int {
	w := math.Sqrt(12*sigma*sigma/3 + 1)
	wl := int(w)
	if wl%2 == 0 {
		wl--
	}
	wu := wl + 2
	m := int(math.Round((12*sigma*sigma - float64(3*(wl*wl+4*wl+3))) / float64(-4*(wl+1))))
	if m < 0 {
		m = 0
	}
	if m > 3 {
		m = 3
	}
	var r [3]int
	for i := range 3 {
		if i < m {
			r[i] = wl / 2
		} else {
			r[i] = wu / 2
		}
	}
	return r
}

// boxBlurH applies a single horizontal box-blur pass with the given radius.
// Each output pixel is the mean of (2*radius+1) input pixels, clamped at edges.
// Runs in O(width×height) time regardless of radius via a sliding window sum.
func boxBlurH(dst, src []float32, width, height int, radius int) {
	if radius <= 0 {
		copy(dst, src)
		return
	}
	n := float64(2*radius + 1)
	for y := range height {
		srcRow := src[y*width:]
		dstRow := dst[y*width:]
		var sum float64
		for i := -radius; i <= radius; i++ {
			xi := i
			if xi < 0 {
				xi = 0
			} else if xi >= width {
				xi = width - 1
			}
			sum += float64(srcRow[xi])
		}
		for x := range width {
			dstRow[x] = float32(sum / n)
			outX := x - radius
			if outX < 0 {
				outX = 0
			}
			inX := x + radius + 1
			if inX >= width {
				inX = width - 1
			}
			sum += float64(srcRow[inX]) - float64(srcRow[outX])
		}
	}
}

// boxBlurV applies a single vertical box-blur pass with the given radius.
// Runs in O(width×height) time regardless of radius via a sliding window sum.
func boxBlurV(dst, src []float32, width, height int, radius int) {
	if radius <= 0 {
		copy(dst, src)
		return
	}
	n := float64(2*radius + 1)
	for x := range width {
		var sum float64
		for i := -radius; i <= radius; i++ {
			yi := i
			if yi < 0 {
				yi = 0
			} else if yi >= height {
				yi = height - 1
			}
			sum += float64(src[yi*width+x])
		}
		for y := range height {
			dst[y*width+x] = float32(sum / n)
			outY := y - radius
			if outY < 0 {
				outY = 0
			}
			inY := y + radius + 1
			if inY >= height {
				inY = height - 1
			}
			sum += float64(src[inY*width+x]) - float64(src[outY*width+x])
		}
	}
}

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
