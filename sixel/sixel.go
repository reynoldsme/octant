// Package sixel encodes images as DEC sixel data suitable for terminals that
// support the sixel graphics protocol (e.g. xterm, mlterm, foot).
//
// The encoder is optimized for oscilloscope-style images: a dark background
// with a narrow, brightly-colored trace and diffuse glow. It derives a
// uniform brightness ramp from the dominant non-black hue.
package sixel

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"runtime"
	"sync"
)

// Encoder encodes images to sixel format.
type Encoder struct {
	// NumColors is the number of palette entries (max 256). Default 64.
	NumColors int
}

// bandBufPool recycles per-band output byte slices across Encode calls to
// reduce GC pressure in continuous rendering loops.
var bandBufPool sync.Pool

// Encode writes img to w as a sixel image using default settings.
func Encode(w io.Writer, img image.Image) error {
	return (&Encoder{NumColors: 64}).Encode(w, img)
}

// Encode encodes img and writes the sixel stream to w.
func (e *Encoder) Encode(w io.Writer, img image.Image) error {
	numColors := e.NumColors
	if numColors <= 0 {
		numColors = 64
	}
	if numColors > 256 {
		numColors = 256
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width == 0 || height == 0 {
		return nil
	}

	palette := buildPalette(img, numColors)

	// Map every pixel to its nearest palette index (parallel).
	mapped := make([]uint8, width*height)
	mapPixels(img, mapped, palette, width, height, bounds)

	bw := bufio.NewWriterSize(w, 1<<16)

	// DCS introducer + raster attributes.
	fmt.Fprintf(bw, "\x1bP0;1q\"1;1;%d;%d", width, height)

	// Color definitions.
	for i, c := range palette {
		r := int(c.R) * 100 / 255
		g := int(c.G) * 100 / 255
		b := int(c.B) * 100 / 255
		fmt.Fprintf(bw, "#%d;2;%d;%d;%d", i, r, g, b)
	}

	// Encode bands in parallel, then write in order.
	numBands := (height + 5) / 6
	bandBufs := make([][]byte, numBands)

	nw := runtime.NumCPU()
	if nw > numBands {
		nw = numBands
	}
	jobs := make(chan int, numBands)
	for b := range numBands {
		jobs <- b
	}
	close(jobs)

	var wg sync.WaitGroup
	for range nw {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nc := len(palette)
			bandBits := make([]byte, nc*width)
			hasColor := make([]bool, nc)
			for band := range jobs {
				bandBufs[band] = encodeBand(band, mapped, palette, width, height, bandBits, hasColor)
			}
		}()
	}
	wg.Wait()

	for _, buf := range bandBufs {
		bw.Write(buf)
		bandBufPool.Put(buf[:0])
	}
	bw.WriteString("\x1b\\") // string terminator
	return bw.Flush()
}

// mapPixels maps every pixel to its nearest palette index in parallel.
// It has a fast path for *image.RGBA that avoids interface dispatch.
func mapPixels(img image.Image, mapped []uint8, palette []color.RGBA, width, height int, bounds image.Rectangle) {
	nw := runtime.NumCPU()
	if nw > height {
		nw = height
	}
	rowsPerWorker := (height + nw - 1) / nw

	var wg sync.WaitGroup
	switch src := img.(type) {
	case *image.RGBA:
		scale := float64(len(palette)-1) / 255.0
		maxIdx := len(palette) - 1
		xOff := (bounds.Min.X - src.Rect.Min.X) * 4
		for w := range nw {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				yStart := id * rowsPerWorker
				yEnd := min(yStart+rowsPerWorker, height)
				for y := yStart; y < yEnd; y++ {
					dst := mapped[y*width : y*width+width]
					srcRow := (bounds.Min.Y + y - src.Rect.Min.Y) * src.Stride
					for x := range width {
						p := src.Pix[srcRow+xOff+x*4:]
						lum := 0.2126*float64(p[0]) + 0.7152*float64(p[1]) + 0.0722*float64(p[2])
						idx := int(lum*scale + 0.5)
						if idx > maxIdx {
							idx = maxIdx
						}
						dst[x] = uint8(idx)
					}
				}
			}(w)
		}
	default:
		for w := range nw {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				yStart := id * rowsPerWorker
				yEnd := min(yStart+rowsPerWorker, height)
				for y := yStart; y < yEnd; y++ {
					dst := mapped[y*width : y*width+width]
					for x := range width {
						dst[x] = nearestPalette(img.At(x+bounds.Min.X, y+bounds.Min.Y), palette)
					}
				}
			}(w)
		}
	}
	wg.Wait()
}

// encodeBand encodes one sixel band (6 rows) into a fresh []byte.
// bandBits and hasColor are per-worker scratch buffers cleared on entry.
//
// Instead of iterating numColors×width×6 (checking each color at each
// pixel), we do a single row-major pass over mapped to build bandBits,
// then one pass per present color to emit. For 64 colors this is ~5× fewer
// inner iterations.
func encodeBand(band int, mapped []uint8, palette []color.RGBA, width, height int, bandBits []byte, hasColor []bool) []byte {
	y0 := band * 6
	nc := len(palette)

	clear(bandBits[:nc*width])
	clear(hasColor[:nc])

	// Build sixel bit masks in row-major order (good cache locality).
	for row := range 6 {
		y := y0 + row
		if y >= height {
			break
		}
		bit := byte(1 << uint(row))
		rowBase := y * width
		for x := range width {
			ci := int(mapped[rowBase+x])
			bandBits[ci*width+x] |= bit
			hasColor[ci] = true
		}
	}

	// Emit: skip palette[0] (black background — implicit default).
	need := nc * width
	var buf []byte
	if v := bandBufPool.Get(); v != nil {
		buf = v.([]byte)
		if cap(buf) < need {
			buf = make([]byte, 0, need)
		}
	} else {
		buf = make([]byte, 0, need)
	}
	for ci := 1; ci < nc; ci++ {
		if !hasColor[ci] {
			continue
		}
		buf = appendUint(buf, '#', ci)
		off := ci * width
		base := len(buf)
		buf = append(buf, bandBits[off:off+width]...)
		for i := range width {
			buf[base+i] += '?'
		}
		buf = append(buf, '$') // carriage return: next color starts at x=0
	}
	buf = append(buf, '-') // line feed: advance to next band
	return buf
}

// appendUint appends prefix then the decimal representation of n to buf.
func appendUint(buf []byte, prefix byte, n int) []byte {
	buf = append(buf, prefix)
	if n < 10 {
		return append(buf, byte('0'+n))
	}
	start := len(buf)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := start, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return buf
}

// buildPalette creates a brightness ramp from black to the dominant hue.
// This works well for phosphor-style images where all non-black pixels share
// one hue family.
func buildPalette(img image.Image, numColors int) []color.RGBA {
	bounds := img.Bounds()
	width := bounds.Dx()

	// Sample the image to find the luminance-weighted average non-dark color.
	var rSum, gSum, bSum, wSum float64
	step := 1
	total := width * bounds.Dy()
	if total > 20000 {
		step = total / 20000
	}

	switch src := img.(type) {
	case *image.RGBA:
		xOff := (bounds.Min.X - src.Rect.Min.X) * 4
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			rowOff := (y-src.Rect.Min.Y)*src.Stride + xOff
			for x := 0; x < width; x += step {
				p := src.Pix[rowOff+x*4:]
				rf, gf, bf := float64(p[0]), float64(p[1]), float64(p[2])
				lum := 0.2126*rf + 0.7152*gf + 0.0722*bf
				if lum > 10 {
					rSum += rf * lum
					gSum += gf * lum
					bSum += bf * lum
					wSum += lum
				}
			}
		}
	default:
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x += step {
				r, g, b, _ := img.At(x, y).RGBA()
				rf := float64(r >> 8)
				gf := float64(g >> 8)
				bf := float64(b >> 8)
				lum := 0.2126*rf + 0.7152*gf + 0.0722*bf
				if lum > 10 {
					rSum += rf * lum
					gSum += gf * lum
					bSum += bf * lum
					wSum += lum
				}
			}
		}
	}

	palette := make([]color.RGBA, numColors)
	// palette[0] is always black (background).
	if wSum == 0 {
		return palette
	}

	// Normalize to get dominant color at unit brightness.
	rN := rSum / wSum
	gN := gSum / wSum
	bN := bSum / wSum
	maxC := math.Max(rN, math.Max(gN, bN))
	if maxC > 0 {
		rN /= maxC
		gN /= maxC
		bN /= maxC
	}

	// Uniform ramp: palette[0]=black, palette[numColors-1]=full dominant color.
	for i := 1; i < numColors; i++ {
		t := float64(i) / float64(numColors-1)
		palette[i] = color.RGBA{
			R: uint8(rN * t * 255),
			G: uint8(gN * t * 255),
			B: uint8(bN * t * 255),
			A: 255,
		}
	}
	return palette
}

// nearestPalette returns the palette index whose brightness best matches c.
// Since all palette entries share one hue, luminance is the only discriminant.
func nearestPalette(c color.Color, palette []color.RGBA) uint8 {
	r, g, b, _ := c.RGBA()
	lum := 0.2126*float64(r>>8) + 0.7152*float64(g>>8) + 0.0722*float64(b>>8)
	idx := int(lum/255.0*float64(len(palette)-1) + 0.5)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(palette) {
		idx = len(palette) - 1
	}
	return uint8(idx)
}
