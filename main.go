package main

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/ericpauley/go-quantize/quantize"
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
	// sRGB → linear light lookup table
	for i := range 256 {
		c := float64(i) / 255.0
		if c <= 0.04045 {
			srgbLinLUT[i] = c / 12.92
		} else {
			srgbLinLUT[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}

	// rawOctants is the 232-entry sequence of Unicode 16.0 octant characters.
	// It was built with rows in the wrong order (row 0 in high bits), so each
	// index is permuted to the correct bit layout before being stored.
	rawOctants := []rune{
		' ', '𜴀', '𜴃', '𜴉', '𜴘', '𜴄', '𜴊', '𜴙', '𜴶', '𜵱',
		'𜴁', '𜴋', '𜴚', '𜴷', '𜵲', '𜴆', '𜴜', '𜴹', '𜵴', '𜴐',
		'𜴽', '𜵸', '𜴧', '𜶀', '𜵑', '𜴂', '𜴅', '𜴌', '𜴛', '𜴸',
		'𜵳', '𜴇', '𜴍', '𜴝', '𜴺', '𜵵', '𜴑', '𜴠', '𜴾', '𜵹',
		'𜴨', '𜵅', '𜶁', '𜵒', '𜶐', '𜶬', '𜴈', '𜴎', '𜴞', '𜴻',
		'𜵶', '𜴒', '𜴡', '𜴿', '𜵺', '𜴩', '𜵆', '𜶂', '𜵓', '𜶑',
		'𜶭', '𜴔', '𜴣', '𜵁', '𜵼', '𜴫', '𜵈', '𜶄', '𜵕', '𜶓',
		'𜶯', '𜴯', '𜵋', '𜶈', '𜵙', '𜶖', '𜶳', '𜵡', '𜶜', '𜶻',
		'𜷋', '𜴏', '𜴟', '𜴼', '𜵷', '𜴓', '𜴢', '𜵀', '𜵻', '𜴪',
		'𜵇', '𜶃', '𜵔', '𜶒', '𜶮', '𜴕', '𜴤', '𜵂', '𜵽', '𜴬',
		'𜶅', '𜵖', '𜶰', '𜴰', '𜵌', '𜶉', '𜵚', '𜶗', '𜶴', '𜵢',
		'𜶝', '𜶼', '𜷌', '𜴖', '𜴥', '𜵃', '𜵾', '𜴭', '𜵉', '𜶆',
		'𜵗', '𜶔', '𜶱', '𜴱', '𜶊', '𜵛', '𜶵', '𜵣', '𜶞', '𜶽',
		'𜷍', '𜴳', '𜵎', '𜶌', '𜵝', '𜶙', '𜶷', '𜵥', '𜶠', '𜶿',
		'𜷏', '𜵩', '𜶤', '𜷃', '𜷓', '𜴗', '𜴦', '𜵄', '𜵿', '𜴮',
		'𜵊', '𜶇', '𜵘', '𜶕', '𜶲', '𜴲', '𜵍', '𜶋', '𜵜', '𜶘',
		'𜶶', '𜵤', '𜶟', '𜶾', '𜷎', '𜴴', '𜵏', '𜶍', '𜵞', '𜶚',
		'𜶸', '𜵦', '𜶡', '𜷀', '𜷐', '𜵪', '𜶥', '𜷄', '𜷔', '𜷛',
		'𜴵', '𜵐', '𜶎', '𜵟', '𜶛', '𜶹', '𜵧', '𜶢', '𜷁', '𜷑',
		'𜵫', '𜶦', '𜷅', '𜷕', '𜷜', '𜵭', '𜶨', '𜷇', '𜷗', '𜷞',
		'𜷡', '𜶏', '𜵠', '𜶺', '𜵨', '𜶣', '𜷂', '𜷒', '𜵬', '𜶧',
		'𜷆', '𜷖', '𜷝', '𜵮', '𜶩', '𜷈', '𜷘', '𜷢', '𜵯', '𜶪',
		'𜷉', '𜷙', '𜷟', '𜵰', '𜶫', '𜷊', '𜷚', '𜷠', '𜷣', '𜷤',
		'𜷥', ' ',
	}

	permute := func(w int) int {
		mapping := [8]uint{6, 7, 4, 5, 2, 3, 0, 1}
		c := 0
		for b := range uint(8) {
			if w&(1<<b) != 0 {
				c |= 1 << mapping[b]
			}
		}
		return c
	}
	for i, r := range rawOctants {
		if r != ' ' && r != 0 {
			octantLookup[permute(i)] = r
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

// perceptualDist returns the perceptually-weighted squared distance between two
// linear RGB colors, using BT.709 luminance coefficients as weights.
func perceptualDist(a, b linRGB) float64 {
	dr := a[0] - b[0]
	dg := a[1] - b[1]
	db := a[2] - b[2]
	return 0.2126*dr*dr + 0.7152*dg*dg + 0.0722*db*db
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

func main() {
	monochrome := false
	maxCols := 0 // 0 = use terminal width
	var filename string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mono" || arg == "-m":
			monochrome = true
		case arg == "--cols" || arg == "-c":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "%s: --cols requires an argument\n", os.Args[0])
				os.Exit(1)
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				fmt.Fprintf(os.Stderr, "%s: invalid --cols value %q\n", os.Args[0], args[i])
				os.Exit(1)
			}
			maxCols = n
		case strings.HasPrefix(arg, "--cols="):
			n, err := strconv.Atoi(arg[len("--cols="):])
			if err != nil || n < 1 {
				fmt.Fprintf(os.Stderr, "%s: invalid --cols value %q\n", os.Args[0], arg)
				os.Exit(1)
			}
			maxCols = n
		case filename == "":
			filename = arg
		}
	}

	if filename == "" {
		fmt.Fprintf(os.Stderr, "usage: %s [--mono] [--cols N] <image.jpg|image.png>\n", os.Args[0])
		os.Exit(1)
	}

	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	if ext != "jpg" && ext != "jpeg" && ext != "png" {
		fmt.Fprintf(os.Stderr, "unsupported format %q: must be jpeg or png\n", ext)
		os.Exit(1)
	}

	f, err := os.Open(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error decoding image:", err)
		os.Exit(1)
	}

	img = scaleImage(img, maxCols)

	if monochrome {
		runMonochrome(img)
	} else {
		runColor(img)
	}
}

// scaleImage resizes img so its width in terminal columns fits within maxCols.
// Each terminal column is 2 image pixels wide (octant cells are 2×4).
// If maxCols is 0, the terminal width is used; if that cannot be determined,
// the image is returned unscaled.
func scaleImage(img image.Image, maxCols int) image.Image {
	if maxCols == 0 {
		w, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil || w <= 0 {
			return img
		}
		maxCols = w
	}

	bounds := img.Bounds()
	imgW, imgH := bounds.Dx(), bounds.Dy()
	maxPixW := maxCols * 2 // each col = 2 pixels

	if imgW <= maxPixW {
		return img
	}

	// Scale proportionally.
	newW := maxPixW
	newH := imgH * newW / imgW
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	// Nearest-neighbour downscale (adequate; images are further quantised anyway).
	for y := range newH {
		for x := range newW {
			srcX := x * imgW / newW
			srcY := y * imgH / newH
			dst.Set(x, y, img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	return dst
}

func runColor(img image.Image) {
	bounds := img.Bounds()
	blockW := (bounds.Dx() + 1) / 2
	blockH := (bounds.Dy() + 3) / 4

	// Per-block accumulated error in linear RGB, propagated Floyd-Steinberg style.
	errBuf := make([]linRGB, blockW*blockH)

	spread := func(bx, by int, err linRGB, weight float64) {
		if bx >= 0 && bx < blockW && by >= 0 && by < blockH {
			errBuf[by*blockW+bx] = errBuf[by*blockW+bx].add(err.scale(weight))
		}
	}

	for by := 0; by < blockH; by++ {
		for bx := 0; bx < blockW; bx++ {
			x := bounds.Min.X + bx*2
			y := bounds.Min.Y + by*4
			fore, back, r, blockErr := processBlock(img, x, y, bounds, errBuf[by*blockW+bx])
			printBlock(fore, back, r)

			spread(bx+1, by, blockErr, 7.0/16)
			spread(bx-1, by+1, blockErr, 3.0/16)
			spread(bx, by+1, blockErr, 5.0/16)
			spread(bx+1, by+1, blockErr, 1.0/16)
		}
		fmt.Println()
	}
}

// processBlock quantizes a 2x4 image region to two colors and returns:
//   - foreground and background colors (sRGB) computed as the mean of their
//     respective classified pixels (improvement #3)
//   - the octant character matching the per-pixel classification
//   - the block error vector (linear RGB) for error diffusion (improvement #2)
//
// Pixels are linearised before all operations (improvement #1), and
// classification uses perceptually-weighted distance (improvement #4).
// The accumulated error from neighbouring blocks is applied before quantising.
func processBlock(img image.Image, x, y int, bounds image.Rectangle, accErr linRGB) (color.Color, color.Color, rune, linRGB) {
	// Collect pixels in linear space and apply accumulated error.
	var orig [8]linRGB
	shifted := image.NewRGBA(image.Rect(0, 0, 2, 4))
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			px, py := x+dx, y+dy
			i := dy*2 + dx
			if px >= bounds.Min.X && px < bounds.Max.X &&
				py >= bounds.Min.Y && py < bounds.Max.Y {
				orig[i] = toLinRGB(img.At(px, py))
			}
			// Shift by accumulated error and convert back to sRGB for the quantizer.
			shifted.Set(dx, dy, orig[i].add(accErr).clamp().toColor())
		}
	}

	// Quantize the error-shifted block to two colors.
	palette := quantize.MedianCutQuantizer{}.Quantize(make([]color.Color, 0, 2), shifted)
	linPal := make([]linRGB, len(palette))
	for i, p := range palette {
		linPal[i] = toLinRGB(p)
	}

	// Classify each pixel, accumulate per-class means, build the octant index.
	// Row 3 → high bits, row 0 → low bits; right col is the higher bit of each pair.
	var foreSum, backSum linRGB
	var foreN, backN float64
	var index int
	for dy := 3; dy >= 0; dy-- {
		for _, dx := range []int{1, 0} {
			i := dy*2 + dx
			// Use the shifted pixel for classification so the error shift
			// influences which cluster each borderline pixel falls into.
			s := orig[i].add(accErr).clamp()
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

	// Use the mean of each class as the displayed color rather than the
	// quantizer's cluster center.
	foreColor := linPal[0]
	if foreN > 0 {
		foreColor = foreSum.scale(1 / foreN)
	}
	backColor := linPal[0]
	if backN > 0 {
		backColor = backSum.scale(1 / backN)
	}

	// Block error = mean(original pixels) − mean(displayed pixels).
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
	return foreColor.toColor(), backColor.toColor(), r, blockErr
}

func runMonochrome(img image.Image) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Convert to linear luminance (BT.709 coefficients).
	lum := make([]float64, w*h)
	for y := range h {
		for x := range w {
			p := toLinRGB(img.At(bounds.Min.X+x, bounds.Min.Y+y))
			lum[y*w+x] = 0.2126*p[0] + 0.7152*p[1] + 0.0722*p[2]
		}
	}

	// Floyd-Steinberg dithering in linear light. The threshold is 0.5 linear
	// (perceptual midpoint), not 128/255 in gamma-encoded space.
	for y := range h {
		for x := range w {
			old := lum[y*w+x]
			var new float64
			if old >= 0.5 {
				new = 1.0
			}
			lum[y*w+x] = new
			e := old - new
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

	// Build and print octants from the binary luminance buffer.
	for y := 0; y < h; y += 4 {
		for x := 0; x < w; x += 2 {
			var index int
			for dy := 3; dy >= 0; dy-- {
				for _, dx := range []int{1, 0} {
					index <<= 1
					nx, ny := x+dx, y+dy
					if nx < w && ny < h && lum[ny*w+nx] > 0.5 {
						index |= 1
					}
				}
			}
			r := octantLookup[index]
			if r == 0 {
				r = ' '
			}
			fmt.Printf("\033[37;40m%c\033[0m", r)
		}
		fmt.Println()
	}
}

// printBlock emits ANSI truecolor escape codes and the octant character.
func printBlock(foreColor, backColor color.Color, octant rune) {
	f := color.RGBAModel.Convert(foreColor).(color.RGBA)
	b := color.RGBAModel.Convert(backColor).(color.RGBA)
	fmt.Printf("\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm%c\033[0m",
		f.R, f.G, f.B, b.R, b.G, b.B, octant)
}
