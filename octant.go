// Package octant renders images as Unicode 2×4 octant-block characters with
// ANSI 24-bit truecolor escape sequences, providing sub-character resolution
// in any terminal that supports Unicode 16.0 (e.g. Cascadia Code / Nerd Fonts).
//
// Each terminal cell is treated as a 2-column × 4-row pixel grid. The 256
// possible fill patterns map directly to Unicode block characters: the 230
// octant characters in U+1CD00–U+1CDE5 plus legacy block-drawing characters
// for patterns they already cover.
//
// Typical usage:
//
//	img, _ := imaging.Open("photo.jpg")
//	octant.Render(img, os.Stdout)
//
// For animated output (e.g. plugging into a game engine):
//
//	t := &octant.Terminal{W: os.Stdout}
//	for each frame:
//	    t.DrawFrame(frameRGBA)
package octant

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	_ "image/jpeg"
	"io"
	"math"
	"os"

	"golang.org/x/term"
)

// srgbLinLUT converts 8-bit sRGB values to linear light [0,1].
var srgbLinLUT [256]float64

// octantLookup maps an 8-bit pixel pattern to its Unicode character.
//
// Bit layout (bit 0 = LSB):
//
//	bit 0 = row 0, col 0  (top-left)
//	bit 1 = row 0, col 1  (top-right)
//	bit 2 = row 1, col 0
//	bit 3 = row 1, col 1
//	bit 4 = row 2, col 0
//	bit 5 = row 2, col 1
//	bit 6 = row 3, col 0
//	bit 7 = row 3, col 1  (bottom-right)
var octantLookup [256]rune

func init() {
	// sRGB → linear light lookup table.
	for i := range 256 {
		c := float64(i) / 255.0
		if c <= 0.04045 {
			srgbLinLUT[i] = c / 12.92
		} else {
			srgbLinLUT[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}

	// Assign U+1CD00..U+1CDE5 to octant patterns not covered by standard
	// block-drawing characters. Unicode 16.0 enumerates these characters in
	// ascending order of their 8-bit pattern (bit 0 = row0/col0, bit 7 =
	// row3/col1), skipping patterns already represented by legacy block chars.
	specialCase := [256]bool{}
	for _, p := range []int{
		0x00, 0x01, 0x02, 0x03, 0x05, 0x0A, 0x0F, 0x14, 0x28, 0x3F,
		0x40, 0x50, 0x55, 0x5A, 0x5F, 0x80, 0xA0, 0xA5, 0xAA, 0xAF,
		0xC0, 0xF0, 0xF5, 0xFA, 0xFC, 0xFF,
	} {
		specialCase[p] = true
	}
	cp := rune(0x1CD00)
	for i := range 256 {
		if !specialCase[i] {
			octantLookup[i] = cp
			cp++
		}
	}

	// Chafa special cases: patterns that map to existing block elements or
	// specific new octant codepoints. Source: hpjansson/chafa.
	octantLookup[0x00] = '\u00A0'     // empty
	octantLookup[0x01] = '\U0001CEA8' // single pixel: row 0, col 0 (top-left)
	octantLookup[0x02] = '\U0001CEAB' // single pixel: row 0, col 1 (top-right)
	octantLookup[0x03] = '\U0001FB82' // top row filled
	octantLookup[0x05] = '\u2598'     // ▘ upper-left quadrant
	octantLookup[0x0A] = '\u259D'     // ▝ upper-right quadrant
	octantLookup[0x0F] = '\u2580'     // ▀ upper half block
	octantLookup[0x14] = '\U0001FBE6'
	octantLookup[0x28] = '\U0001FBE7'
	octantLookup[0x3F] = '\U0001FB85' // upper three-quarters block
	octantLookup[0x40] = '\U0001CEA3' // single pixel: row 3, col 0 (bottom-left)
	octantLookup[0x50] = '\u2596'     // ▖ lower-left quadrant
	octantLookup[0x55] = '\u258C'     // ▌ left half block
	octantLookup[0x5A] = '\u259E'     // ▚
	octantLookup[0x5F] = '\u259B'     // ▛
	octantLookup[0x80] = '\U0001CEA0' // single pixel: row 3, col 1 (bottom-right)
	octantLookup[0xA0] = '\u2597'     // ▗ lower-right quadrant
	octantLookup[0xA5] = '\u259A'     // ▚
	octantLookup[0xAA] = '\u2590'     // ▐ right half block
	octantLookup[0xAF] = '\u259C'     // ▜
	octantLookup[0xC0] = '\u2582'     // ▂ lower one-quarter block (bottom row)
	octantLookup[0xF0] = '\u2584'     // ▄ lower half block
	octantLookup[0xF5] = '\u2599'     // ▙
	octantLookup[0xFA] = '\u259F'     // ▟
	octantLookup[0xFC] = '\u2586'     // ▆ lower three-quarters block
	octantLookup[0xFF] = '\u2588'     // █ full block
}

// Terminal is a stateful renderer that writes octant-block output to W.
// Successive DrawFrame calls overwrite the previous frame in place by moving
// the cursor to the top-left of the terminal before each write, making it
// suitable for real-time animation in raw terminal mode.
//
// Terminal.DrawFrame has the same signature as the DrawFrame method of
// gore.DoomFrontend, so a *Terminal can be embedded in a Gore frontend struct
// with no adapter code.
type Terminal struct {
	// W is the destination writer. Defaults to os.Stdout if nil.
	W io.Writer
	// MaxCols is the maximum output width in terminal columns (each column is
	// 2 pixels wide). 0 auto-detects from W when W is an *os.File, falling
	// back to os.Stdout.
	MaxCols int
	// Mono selects monochrome (1-bit dithered) output instead of full colour.
	Mono bool
}

// DrawFrame renders img to t.W, overwriting the previous frame if one has
// already been written. The image is scaled to fit MaxCols columns.
//
// Output uses \r\n line endings so that the frame renders correctly in raw
// terminal mode (where OPOST/ONLCR is disabled and bare \n does not return
// the cursor to column 0). The cursor is moved to the top-left (\x1b[H)
// before each frame so successive frames overwrite in place.
//
// This method satisfies the drawing half of gore.DoomFrontend.
func (t *Terminal) DrawFrame(img *image.RGBA) {
	w := t.W
	if w == nil {
		w = os.Stdout
	}
	scaled := Scale(img, t.maxCols(w))
	var buf bytes.Buffer
	if t.Mono {
		RenderMono(scaled, &buf)
	} else {
		Render(scaled, &buf)
	}
	// Replace \n with \r\n: in raw terminal mode OPOST is disabled so \n alone
	// only moves the cursor down without returning to column 0. \r\n is safe in
	// both raw and cooked modes.
	out := bytes.ReplaceAll(buf.Bytes(), []byte("\n"), []byte("\r\n"))
	fmt.Fprintf(w, "\x1b[H")
	w.Write(out)
}

// maxCols resolves the effective column limit for writer w.
func (t *Terminal) maxCols(w io.Writer) int {
	if t.MaxCols > 0 {
		return t.MaxCols
	}
	return writerTermWidth(w)
}

// writerTermWidth returns the terminal width of w if it is an *os.File,
// otherwise falls back to os.Stdout, returning 0 if neither works.
func writerTermWidth(w io.Writer) int {
	for _, f := range []*os.File{toFile(w), os.Stdout} {
		if f != nil {
			if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
				return width
			}
		}
	}
	return 0
}

func toFile(w io.Writer) *os.File {
	f, _ := w.(*os.File)
	return f
}

// linRGB is a color in linear light RGB [0,1].
type linRGB [3]float64

func toLinRGB(c color.Color) linRGB {
	r, g, b, _ := c.RGBA()
	return linRGB{srgbLinLUT[r>>8], srgbLinLUT[g>>8], srgbLinLUT[b>>8]}
}

func (p linRGB) toColor() color.Color {
	return color.RGBA{
		R: linToSRGB8(p[0]),
		G: linToSRGB8(p[1]),
		B: linToSRGB8(p[2]),
		A: 255,
	}
}

func (p linRGB) add(q linRGB) linRGB    { return linRGB{p[0] + q[0], p[1] + q[1], p[2] + q[2]} }
func (p linRGB) sub(q linRGB) linRGB    { return linRGB{p[0] - q[0], p[1] - q[1], p[2] - q[2]} }
func (p linRGB) scale(f float64) linRGB { return linRGB{p[0] * f, p[1] * f, p[2] * f} }
func (p linRGB) clamp() linRGB {
	return linRGB{clampF(p[0]), clampF(p[1]), clampF(p[2])}
}

// linToSRGB8 converts a linear light value to an 8-bit sRGB value.
func linToSRGB8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	var c float64
	if v <= 0.0031308 {
		c = v * 12.92
	} else {
		c = 1.055*math.Pow(v, 1.0/2.4) - 0.055
	}
	return uint8(math.Round(c * 255))
}

func clampF(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// perceptualDist returns the perceptually-weighted squared distance between
// two linear RGB colors using BT.709 luminance coefficients.
func perceptualDist(a, b linRGB) float64 {
	dr := a[0] - b[0]
	dg := a[1] - b[1]
	db := a[2] - b[2]
	return 0.2126*dr*dr + 0.7152*dg*dg + 0.0722*db*db
}

// quantize2 partitions pixels into at most 2 clusters using a single k-means
// pass seeded from the pair of pixels with maximum perceptual distance.
// Returns a 1-entry palette when all pixels are the same color.
func quantize2(pixels []linRGB) []linRGB {
	bestDist := -1.0
	s0, s1 := 0, 1
	for i := range pixels {
		for j := i + 1; j < len(pixels); j++ {
			if d := perceptualDist(pixels[i], pixels[j]); d > bestDist {
				bestDist, s0, s1 = d, i, j
			}
		}
	}
	if bestDist == 0 {
		return []linRGB{pixels[0]}
	}
	seed0, seed1 := pixels[s0], pixels[s1]

	var sum0, sum1 linRGB
	var n0, n1 float64
	for _, p := range pixels {
		if perceptualDist(p, seed0) <= perceptualDist(p, seed1) {
			sum0 = sum0.add(p)
			n0++
		} else {
			sum1 = sum1.add(p)
			n1++
		}
	}
	return []linRGB{sum0.scale(1 / n0), sum1.scale(1 / n1)}
}

// nearestLinRGB returns the index of the closest entry in palette to p.
func nearestLinRGB(p linRGB, palette []linRGB) int {
	best, bestDist := 0, math.MaxFloat64
	for i, q := range palette {
		if d := perceptualDist(p, q); d < bestDist {
			bestDist, best = d, i
		}
	}
	return best
}

// Scale resizes img proportionally so its width fits within maxCols terminal
// columns (each column is 2 source pixels wide). When maxCols is 0 the width
// is auto-detected from os.Stdout; if that fails img is returned unchanged.
func Scale(img image.Image, maxCols int) image.Image {
	if maxCols == 0 {
		maxCols = writerTermWidth(os.Stdout)
		if maxCols == 0 {
			return img
		}
	}

	bounds := img.Bounds()
	imgW, imgH := bounds.Dx(), bounds.Dy()
	maxPixW := maxCols * 2 // each column = 2 source pixels

	if imgW <= maxPixW {
		return img
	}

	newW := maxPixW
	newH := imgH * newW / imgW
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	// Box-filter (area-average) downscale to avoid aliasing of narrow vertical
	// features that nearest-neighbour sampling would drop entirely.
	for y := range newH {
		y0 := y * imgH / newH
		y1 := (y+1) * imgH / newH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := range newW {
			x0 := x * imgW / newW
			x1 := (x+1) * imgW / newW
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rSum, gSum, bSum, n float64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, _ := img.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					rSum += float64(r >> 8)
					gSum += float64(g >> 8)
					bSum += float64(b >> 8)
					n++
				}
			}
			dst.Set(x, y, color.RGBA{
				R: uint8(rSum / n), G: uint8(gSum / n), B: uint8(bSum / n), A: 255,
			})
		}
	}
	return dst
}

// midtoneFraction returns the fraction of pixels whose linear luminance falls
// in (0.05, 0.75) — used to decide whether to apply inter-block dithering.
func midtoneFraction(img image.Image) float64 {
	bounds := img.Bounds()
	total, mid := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := toLinRGB(img.At(x, y))
			lum := 0.2126*p[0] + 0.7152*p[1] + 0.0722*p[2]
			if lum > 0.05 && lum < 0.75 {
				mid++
			}
			total++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(mid) / float64(total)
}

// Render renders img as octant-block characters with ANSI 24-bit colour to w.
// The image is assumed to already be scaled to the desired output size; use
// Scale first if you need to fit a particular terminal width.
func Render(img image.Image, w io.Writer) {
	bounds := img.Bounds()
	blockW := (bounds.Dx() + 1) / 2
	blockH := (bounds.Dy() + 3) / 4

	dither := midtoneFraction(img) > 0.02
	errBuf := make([]linRGB, blockW*blockH)

	spread := func(bx, by int, err linRGB, weight float64) {
		if !dither {
			return
		}
		if bx >= 0 && bx < blockW && by >= 0 && by < blockH {
			errBuf[by*blockW+bx] = errBuf[by*blockW+bx].add(err.scale(weight))
		}
	}

	for by := 0; by < blockH; by++ {
		for bx := 0; bx < blockW; bx++ {
			x := bounds.Min.X + bx*2
			y := bounds.Min.Y + by*4
			fore, back, r, _, blockErr := processBlock(img, x, y, bounds, errBuf[by*blockW+bx])
			printBlock(w, fore, back, r)

			spread(bx+1, by, blockErr, 7.0/16)
			spread(bx-1, by+1, blockErr, 3.0/16)
			spread(bx, by+1, blockErr, 5.0/16)
			spread(bx+1, by+1, blockErr, 1.0/16)
		}
		fmt.Fprintln(w)
	}
}

// processBlock quantizes a 2×4 image region to two colours and returns the
// foreground colour, background colour, octant character, 8-bit index, and
// Floyd-Steinberg error vector for the block.
func processBlock(img image.Image, x, y int, bounds image.Rectangle, accErr linRGB) (color.Color, color.Color, rune, int, linRGB) {
	var orig [8]linRGB
	var shifted [8]linRGB
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			px, py := x+dx, y+dy
			i := dy*2 + dx
			if px >= bounds.Min.X && px < bounds.Max.X &&
				py >= bounds.Min.Y && py < bounds.Max.Y {
				orig[i] = toLinRGB(img.At(px, py))
			}
			shifted[i] = orig[i].add(accErr).clamp()
		}
	}

	linPal := quantize2(shifted[:])

	// Normalize: linPal[0] is the lighter colour (background convention).
	if len(linPal) > 1 {
		lum0 := 0.2126*linPal[0][0] + 0.7152*linPal[0][1] + 0.0722*linPal[0][2]
		lum1 := 0.2126*linPal[1][0] + 0.7152*linPal[1][1] + 0.0722*linPal[1][2]
		if lum0 < lum1 {
			linPal[0], linPal[1] = linPal[1], linPal[0]
		}
	}

	var foreSum, backSum linRGB
	var foreN, backN float64
	var index int
	for dy := 3; dy >= 0; dy-- {
		for _, dx := range []int{1, 0} {
			i := dy*2 + dx
			s := shifted[i]
			index <<= 1
			class := 0
			if len(linPal) > 1 {
				class = nearestLinRGB(s, linPal)
			}
			if class == 1 {
				index |= 1
				foreSum = foreSum.add(s)
				foreN++
			} else {
				backSum = backSum.add(s)
				backN++
			}
		}
	}

	foreColor := linPal[0]
	if foreN > 0 {
		foreColor = foreSum.scale(1 / foreN)
	}
	backColor := linPal[0]
	if backN > 0 {
		backColor = backSum.scale(1 / backN)
	}

	var origSum linRGB
	for i := range 8 {
		origSum = origSum.add(orig[i])
	}
	origMean := origSum.scale(1.0 / 8)
	dispMean := foreColor.scale(foreN / 8).add(backColor.scale(backN / 8))
	blockErr := origMean.sub(dispMean)

	r := octantLookup[index]
	if r == 0 {
		r = ' '
	}
	return foreColor.toColor(), backColor.toColor(), r, index, blockErr
}

// RenderMono renders img as monochrome (1-bit Floyd-Steinberg dithered) octant
// blocks to w.
func RenderMono(img image.Image, w io.Writer) {
	bounds := img.Bounds()
	iw, ih := bounds.Dx(), bounds.Dy()

	lum := make([]float64, iw*ih)
	for y := range ih {
		for x := range iw {
			p := toLinRGB(img.At(bounds.Min.X+x, bounds.Min.Y+y))
			lum[y*iw+x] = 0.2126*p[0] + 0.7152*p[1] + 0.0722*p[2]
		}
	}

	for y := range ih {
		for x := range iw {
			old := lum[y*iw+x]
			var nv float64
			if old >= 0.5 {
				nv = 1.0
			}
			lum[y*iw+x] = nv
			e := old - nv
			if x+1 < iw {
				lum[y*iw+x+1] = clampF(lum[y*iw+x+1] + e*7/16)
			}
			if x > 0 && y+1 < ih {
				lum[(y+1)*iw+x-1] = clampF(lum[(y+1)*iw+x-1] + e*3/16)
			}
			if y+1 < ih {
				lum[(y+1)*iw+x] = clampF(lum[(y+1)*iw+x] + e*5/16)
			}
			if x+1 < iw && y+1 < ih {
				lum[(y+1)*iw+x+1] = clampF(lum[(y+1)*iw+x+1] + e*1/16)
			}
		}
	}

	for y := 0; y < ih; y += 4 {
		for x := 0; x < iw; x += 2 {
			var index int
			for dy := 3; dy >= 0; dy-- {
				for _, dx := range []int{1, 0} {
					index <<= 1
					nx, ny := x+dx, y+dy
					if nx < iw && ny < ih && lum[ny*iw+nx] > 0.5 {
						index |= 1
					}
				}
			}
			r := octantLookup[index]
			if r == 0 {
				r = ' '
			}
			fmt.Fprintf(w, "\033[37;40m%c\033[0m", r)
		}
		fmt.Fprintln(w)
	}
}

// printBlock emits ANSI truecolor escape codes and the octant character to w.
func printBlock(w io.Writer, foreColor, backColor color.Color, octant rune) {
	f := color.RGBAModel.Convert(foreColor).(color.RGBA)
	b := color.RGBAModel.Convert(backColor).(color.RGBA)
	fmt.Fprintf(w, "\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm%c\033[0m",
		f.R, f.G, f.B, b.R, b.G, b.B, octant)
}

// RenderToPNG writes the octant-quantised rendering of img to a PNG file.
// Each 2×4 source pixel block becomes a 2×4 pixel region in the output, so
// the output dimensions equal the source dimensions rounded up to the nearest
// 2×4 boundary.
func RenderToPNG(img image.Image, outPath string, monochrome bool) error {
	bounds := img.Bounds()
	imgW, imgH := bounds.Dx(), bounds.Dy()
	blockW := (imgW + 1) / 2
	blockH := (imgH + 3) / 4

	out := image.NewRGBA(image.Rect(0, 0, blockW*2, blockH*4))

	if monochrome {
		w, h := imgW, imgH
		lum := make([]float64, w*h)
		for y := range h {
			for x := range w {
				p := toLinRGB(img.At(bounds.Min.X+x, bounds.Min.Y+y))
				lum[y*w+x] = 0.2126*p[0] + 0.7152*p[1] + 0.0722*p[2]
			}
		}
		for y := range h {
			for x := range w {
				old := lum[y*w+x]
				var nv float64
				if old >= 0.5 {
					nv = 1.0
				}
				lum[y*w+x] = nv
				e := old - nv
				if x+1 < w {
					lum[y*w+x+1] = clampF(lum[y*w+x+1] + e*7/16)
				}
				if x > 0 && y+1 < h {
					lum[(y+1)*w+x-1] = clampF(lum[(y+1)*w+x-1] + e*3/16)
				}
				if y+1 < h {
					lum[(y+1)*w+x] = clampF(lum[(y+1)*w+x] + e*5/16)
				}
				if x+1 < w && y+1 < h {
					lum[(y+1)*w+x+1] = clampF(lum[(y+1)*w+x+1] + e*1/16)
				}
			}
		}
		for by := range blockH {
			for bx := range blockW {
				var index int
				for dy := 3; dy >= 0; dy-- {
					for _, dx := range []int{1, 0} {
						index <<= 1
						nx, ny := bx*2+dx, by*4+dy
						if nx < w && ny < h && lum[ny*w+nx] > 0.5 {
							index |= 1
						}
					}
				}
				for dy := range 4 {
					for dx := range 2 {
						bit := dy*2 + dx
						var c color.Color
						if index&(1<<bit) != 0 {
							c = color.White
						} else {
							c = color.Black
						}
						out.Set(bx*2+dx, by*4+dy, c)
					}
				}
			}
		}
	} else {
		dither := midtoneFraction(img) > 0.02
		errBuf := make([]linRGB, blockW*blockH)
		spread := func(bx, by int, err linRGB, weight float64) {
			if !dither {
				return
			}
			if bx >= 0 && bx < blockW && by >= 0 && by < blockH {
				errBuf[by*blockW+bx] = errBuf[by*blockW+bx].add(err.scale(weight))
			}
		}
		for by := range blockH {
			for bx := range blockW {
				x := bounds.Min.X + bx*2
				y := bounds.Min.Y + by*4
				fore, back, _, idx, blockErr := processBlock(img, x, y, bounds, errBuf[by*blockW+bx])

				spread(bx+1, by, blockErr, 7.0/16)
				spread(bx-1, by+1, blockErr, 3.0/16)
				spread(bx, by+1, blockErr, 5.0/16)
				spread(bx+1, by+1, blockErr, 1.0/16)

				foreRGBA := color.RGBAModel.Convert(fore).(color.RGBA)
				backRGBA := color.RGBAModel.Convert(back).(color.RGBA)
				for dy := range 4 {
					for dx := range 2 {
						bit := dy*2 + dx
						var c color.Color
						if idx&(1<<bit) != 0 {
							c = foreRGBA
						} else {
							c = backRGBA
						}
						out.Set(bx*2+dx, by*4+dy, c)
					}
				}
			}
		}
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

// ComposeGIFFrames composites all frames of g onto a canvas, respecting each
// frame's disposal method, and returns one fully-composited image per frame.
func ComposeGIFFrames(g *gif.GIF) []image.Image {
	width, height := g.Config.Width, g.Config.Height
	if width == 0 || height == 0 {
		b := g.Image[0].Bounds()
		width, height = b.Max.X, b.Max.Y
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)

	frames := make([]image.Image, len(g.Image))
	var saved *image.RGBA

	for i, frame := range g.Image {
		if i > 0 {
			prevDisposal := byte(0)
			if i-1 < len(g.Disposal) {
				prevDisposal = g.Disposal[i-1]
			}
			switch prevDisposal {
			case 2:
				draw.Draw(canvas, g.Image[i-1].Bounds(), image.White, image.Point{}, draw.Src)
			case 3:
				if saved != nil {
					draw.Draw(canvas, canvas.Bounds(), saved, image.Point{}, draw.Src)
				}
			}
		}

		curDisposal := byte(0)
		if i < len(g.Disposal) {
			curDisposal = g.Disposal[i]
		}
		if curDisposal == 3 {
			saved = image.NewRGBA(canvas.Bounds())
			draw.Draw(saved, canvas.Bounds(), canvas, image.Point{}, draw.Src)
		}

		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)

		snap := image.NewRGBA(canvas.Bounds())
		draw.Draw(snap, canvas.Bounds(), canvas, image.Point{}, draw.Src)
		frames[i] = snap
	}
	return frames
}
