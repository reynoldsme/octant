package oscilloscope

import (
	"image"
	"image/color"
)

// drawGrid overlays a CRT-style graticule onto img.
// Draws 10 major divisions in each axis, plus minor tick marks at 5×
// subdivision on the center axes.
// cfg.GridMode: 0 = dark grid (dims phosphor), 1 = phosphor-colored lines.
func drawGrid(img *image.RGBA, cfg Config) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	if cfg.GridMode == 1 {
		// Colored mode: draw fixed-color lines over the phosphor.
		r, g, b := hsvToRGB(cfg.Hue, 0.6, 0.3)
		gridColor := color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 255}
		dimColor := color.RGBA{uint8(r * 255 * 0.4), uint8(g * 255 * 0.4), uint8(b * 255 * 0.4), 255}

		for div := 1; div < 10; div++ {
			x := w * div / 10
			y := h * div / 10
			for py := range h {
				img.SetRGBA(x, py, gridColor)
			}
			for px := range w {
				img.SetRGBA(px, y, gridColor)
			}
		}
		for x := range w {
			img.SetRGBA(x, 0, gridColor)
			img.SetRGBA(x, h-1, gridColor)
		}
		for y := range h {
			img.SetRGBA(0, y, gridColor)
			img.SetRGBA(w-1, y, gridColor)
		}
		tickLen := h / 40
		if tickLen < 2 {
			tickLen = 2
		}
		cx, cy := w/2, h/2
		for tick := range 50 {
			tx := w * tick / 50
			ty := h * tick / 50
			for dy := -tickLen / 2; dy <= tickLen/2; dy++ {
				if py := cy + dy; py >= 0 && py < h {
					img.SetRGBA(tx, py, dimColor)
				}
			}
			for dx := -tickLen / 2; dx <= tickLen/2; dx++ {
				if px := cx + dx; px >= 0 && px < w {
					img.SetRGBA(px, ty, dimColor)
				}
			}
		}
		return
	}

	// Dark mode (GridMode==0): dim pixels in place so the phosphor glow shows
	// through the grid rather than being replaced by solid black.
	dimAt := func(x, y int) {
		if x < 0 || x >= w || y < 0 || y >= h {
			return
		}
		p := img.RGBAAt(x, y)
		img.SetRGBA(x, y, color.RGBA{p.R / 5, p.G / 5, p.B / 5, 255})
	}

	for div := 1; div < 10; div++ {
		x := w * div / 10
		y := h * div / 10
		for py := range h {
			dimAt(x, py)
		}
		for px := range w {
			dimAt(px, y)
		}
	}
	for x := range w {
		dimAt(x, 0)
		dimAt(x, h-1)
	}
	for y := range h {
		dimAt(0, y)
		dimAt(w-1, y)
	}
	tickLen := h / 40
	if tickLen < 2 {
		tickLen = 2
	}
	cx, cy := w/2, h/2
	for tick := range 50 {
		tx := w * tick / 50
		ty := h * tick / 50
		for dy := -tickLen / 2; dy <= tickLen/2; dy++ {
			if py := cy + dy; py >= 0 && py < h {
				dimAt(tx, py)
			}
		}
		for dx := -tickLen / 2; dx <= tickLen/2; dx++ {
			if px := cx + dx; px >= 0 && px < w {
				dimAt(px, ty)
			}
		}
	}
}
