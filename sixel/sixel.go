// Package sixel encodes images as DEC sixel data suitable for terminals that
// support the sixel graphics protocol (e.g. xterm, mlterm, foot).
//
// The encoder is optimized for oscilloscope-style images: a dark background
// with a narrow, brightly-colored trace and diffuse glow. It derives a
// uniform brightness ramp from the dominant non-black hue and uses run-length
// encoding to keep output compact.
package sixel

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
)

// Encoder encodes images to sixel format.
type Encoder struct {
	// NumColors is the number of palette entries (max 256). Default 64.
	NumColors int
}

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

	// Map every pixel to its nearest palette index.
	mapped := make([]uint8, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := mapped[(y-bounds.Min.Y)*width:]
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			row[x-bounds.Min.X] = nearestPalette(img.At(x, y), palette)
		}
	}

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

	// Encode bands of 6 rows.
	for band := 0; band*6 < height; band++ {
		y0 := band * 6

		for ci := range palette {
			// Skip palette entry 0 (black background): it is the default,
			// and we can save bytes by never explicitly drawing it.
			if ci == 0 {
				continue
			}

			var wroteAny bool
			var runCh byte
			var runLen int

			for x := range width {
				// Build the 6-bit mask for this column and color.
				var bits byte
				for row := range 6 {
					y := y0 + row
					if y < height && mapped[y*width+x] == uint8(ci) {
						bits |= 1 << uint(row)
					}
				}
				ch := byte('?') + bits

				if !wroteAny {
					fmt.Fprintf(bw, "#%d", ci)
					runCh = ch
					runLen = 1
					wroteAny = true
				} else if ch == runCh {
					runLen++
				} else {
					writeRun(bw, runCh, runLen)
					runCh = ch
					runLen = 1
				}
			}
			if wroteAny {
				writeRun(bw, runCh, runLen)
				bw.WriteByte('$') // carriage return: next color starts at x=0
			}
		}
		bw.WriteByte('-') // line feed: advance to next band
	}

	bw.WriteString("\x1b\\") // string terminator
	return bw.Flush()
}

func writeRun(w *bufio.Writer, ch byte, count int) {
	if count == 1 {
		w.WriteByte(ch)
	} else {
		fmt.Fprintf(w, "!%d%c", count, ch)
	}
}

// buildPalette creates a brightness ramp from black to the dominant hue.
// This works well for phosphor-style images where all non-black pixels share
// one hue family.
func buildPalette(img image.Image, numColors int) []color.RGBA {
	bounds := img.Bounds()

	// Sample the image to find the luminance-weighted average non-dark color.
	var rSum, gSum, bSum, wSum float64
	step := 1
	total := bounds.Dx() * bounds.Dy()
	if total > 20000 {
		step = total / 20000
	}
	n := 0
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
			n++
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
