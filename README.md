# Octant

Octant is a Go CLI tool and library for rendering images and GIF animations in the terminal using Unicode 16.0
[octant block characters](https://www.unicode.org/charts/PDF/Unicode-16.0/U160-1CC00.pdf)
with ANSI 24-bit truecolor.

Each terminal cell is treated as a **2-column x 4-row pixel grid**. The 256 possible fill patterns map to Unicode block characters — the 230 octant characters in `U+1CD00`–`U+1CDE5` plus legacy block-drawing characters for the patterns they already cover. This gives twice the horizontal and four times the vertical resolution of plain half-block rendering, at the cost of each cell being limited to two colors.

![image](images/example-tsunami.jpg)

Yes, you can [run doom](#octantgore--doom-in-the-terminal)!
![image](images/example-octantgore.jpg)


---

## Unicode 16.0 Font Requirement

Your terminal must use a font that includes the Unicode 16.0 octant characters. These are new enough (circa ~2024) your system may not ship with them. 
[Cascadia Code](https://github.com/microsoft/cascadia-code/releases) includes these. You may use a tool like [getnf](https://github.com/getnf/getnf) to download and install the font, or or install the `.ttf` from the latest GitHub release. Most recently updated Nerd Fonts appear to now include these. Some terminals seem to automatically fall back on other installed fonts for missing glyphs, but when in doubt, select that font directly.

---

## CLI

### Install

```shell
go install github.com/reynoldsme/octant/cmd/octant@latest
```

Or build from source:

```shell
git clone git@github.com:reynoldsme/octant.git
cd octant
go build -o octant ./cmd/octant/
```

### Usage

```shell
octant [--mono] [--cols N] [--png out.png] <image.jpg|image.png|image.gif>
```

| Flag | Description |
|------|-------------|
| `--cols N` / `-c N` | Maximum output width in terminal columns. Defaults to the current terminal width. |
| `--mono` / `-m` | Monochrome output (1-bit Floyd-Steinberg dithered). |
| `--png out.png` | Write the octant-quantised image to a PNG file instead of the terminal. |

**Animated GIFs** play in a loop (respecting the GIF's loop-count setting) until
the program receives `SIGINT` (Ctrl-C).

---

## Library

Import the root package:

```go
import "github.com/reynoldsme/octant"
```

### Rendering a static image

```go
f, _ := os.Open("photo.jpg")
img, _, _ := image.Decode(f)
img = octant.Scale(img, 0) // 0 = auto-detect terminal width
octant.Render(img, os.Stdout)
```

Use `octant.RenderMono` for monochrome output.

### Rendering to a PNG (for testing)

```go
err := octant.RenderToPNG(img, "out.png", false)
```

### Animated output — the `Terminal` type

`Terminal` is a stateful renderer that overwrites the previous frame in place,
making it suitable for real-time animation:

```go
t := &octant.Terminal{W: os.Stdout}

for _, frame := range frames {
    t.DrawFrame(frame) // overwrites previous frame with cursor-up sequences
    time.Sleep(100 * time.Millisecond)
}
```

`Terminal.DrawFrame` accepts `*image.RGBA`, which is the same type and signature
used by [Gore](https://github.com/AndreRenaud/Gore)'s `DoomFrontend` interface —
see `octantgore` below for a complete example.

### API reference

```go
// Render renders img as octant blocks with 24-bit ANSI color to w.
func Render(img image.Image, w io.Writer)

// RenderMono renders img as monochrome (1-bit dithered) octant blocks to w.
func RenderMono(img image.Image, w io.Writer)

// Scale resizes img proportionally to fit maxCols terminal columns.
// maxCols=0 auto-detects the terminal width.
func Scale(img image.Image, maxCols int) image.Image

// RenderToPNG writes the octant rendering of img to a PNG file.
func RenderToPNG(img image.Image, outPath string, monochrome bool) error

// ComposeGIFFrames composites all GIF frames respecting disposal methods,
// returning one fully-composited image per frame.
func ComposeGIFFrames(g *gif.GIF) []image.Image

// Terminal is a stateful renderer for real-time animation.
type Terminal struct {
    W       io.Writer // destination (defaults to os.Stdout)
    MaxCols int       // 0 = auto-detect
    Mono    bool      // monochrome mode
}

// DrawFrame renders img, overwriting the previous frame in place.
func (t *Terminal) DrawFrame(img *image.RGBA)
```

---

## octantgore — DOOM in the terminal

`octantgore` runs DOOM in the terminal using octant block rendering. It requires a DOOM WAD file (e.g. `doom.wad` from a retail or [freedoom release](https://github.com/freedoom/freedoom/releases)).

### Install

```shell
go install github.com/reynoldsme/octant/cmd/octantgore@latest
```

Or build from source:

```shell
git clone git@github.com:reynoldsme/octant.git
cd octant
go build -o octantgore ./cmd/octantgore/
```

### Usage

```
octantgore -iwad doom.wad
```

Keyboard controls:

| Key | Action |
|-----|--------|
| Arrow keys | Move / turn |
| `,` | Fire |
| Space | Use / open |
| Enter | Confirm |
| Escape | Menu / back |
| Tab | Automap |
| `0`–`9` | Cheats / menu selection |

---

## How it works

1. **Scale** — the source image is box-filter downsampled to fit the terminal width
   (each terminal column = 2 source pixels).

2. **Quantize** — each 2×4 pixel block is reduced to two colors using a single
   k-means pass seeded from the most perceptually-distant pixel pair.

3. **Palette normalization** — the lighter color is always used as background and
   the darker as foreground, ensuring canonical characters (e.g. `▌` instead of `▐`
   with a dark background) that render consistently across fonts.

4. **Dithering** — inter-block Floyd-Steinberg error diffusion propagates color
   error to neighboring blocks for smoother gradients. Disabled automatically for
   near-bilevel images to avoid amplifying JPEG artifacts.

5. **Lookup** — the 8-bit block classification index maps directly into a prebuilt
   table of Unicode octant characters, constructed once at `init` time.

---

DOOM is a registered trademark of id Software LLC, a ZeniMax Media company.
This project is not affiliated with or endorsed by id Software or ZeniMax Media.
