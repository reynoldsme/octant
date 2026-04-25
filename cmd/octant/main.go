package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"image/gif"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/reynoldsme/octant"
)

func main() {
	monochrome := false
	maxCols := 0
	var filename string
	var pngOut string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mono" || arg == "-m":
			monochrome = true
		case arg == "--png":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "%s: --png requires an argument\n", os.Args[0])
				os.Exit(1)
			}
			pngOut = args[i]
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
		fmt.Fprintf(os.Stderr, "usage: %s [--mono] [--cols N] [--png out.png] <image.jpg|image.png|image.gif>\n", os.Args[0])
		os.Exit(1)
	}

	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	if ext != "jpg" && ext != "jpeg" && ext != "png" && ext != "gif" {
		fmt.Fprintf(os.Stderr, "unsupported format %q: must be jpeg, png, or gif\n", ext)
		os.Exit(1)
	}

	f, err := os.Open(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	if ext == "gif" {
		g, err := gif.DecodeAll(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error decoding gif:", err)
			os.Exit(1)
		}
		if len(g.Image) == 0 {
			fmt.Fprintln(os.Stderr, "empty gif")
			os.Exit(1)
		}
		frames := octant.ComposeGIFFrames(g)
		if len(frames) == 1 || pngOut != "" {
			img := octant.Scale(frames[0], maxCols)
			if pngOut != "" {
				if err := octant.RenderToPNG(img, pngOut, monochrome); err != nil {
					fmt.Fprintln(os.Stderr, "error writing png:", err)
					os.Exit(1)
				}
				return
			}
			if monochrome {
				octant.RenderMono(img, os.Stdout)
			} else {
				octant.Render(img, os.Stdout)
			}
			return
		}
		runAnimatedGIF(g, frames, maxCols, monochrome)
		return
	}

	img, _, err := image.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error decoding image:", err)
		os.Exit(1)
	}

	img = octant.Scale(img, maxCols)

	if pngOut != "" {
		if err := octant.RenderToPNG(img, pngOut, monochrome); err != nil {
			fmt.Fprintln(os.Stderr, "error writing png:", err)
			os.Exit(1)
		}
		return
	}

	if monochrome {
		octant.RenderMono(img, os.Stdout)
	} else {
		octant.Render(img, os.Stdout)
	}
}

// runAnimatedGIF renders the pre-composited frames to the terminal, cycling
// at each frame's delay. If the GIF's LoopCount is >= 0 the animation loops
// until SIGINT or SIGTERM; otherwise it plays once.
//
// On the first pass each frame is rendered into a bytes.Buffer and cached.
// Subsequent loops write the cached bytes directly, skipping image processing.
func runAnimatedGIF(g *gif.GIF, frames []image.Image, maxCols int, monochrome bool) {
	scaled := make([]image.Image, len(frames))
	for i, f := range frames {
		scaled[i] = octant.Scale(f, maxCols)
	}

	blockH := (scaled[0].Bounds().Dy() + 3) / 4
	cache := make([][]byte, len(scaled))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h\n")

	wait := func(i int) bool {
		delay := g.Delay[i]
		if delay < 2 {
			delay = 2
		}
		select {
		case <-sigs:
			return false
		case <-time.After(time.Duration(delay) * 10 * time.Millisecond):
			return true
		}
	}

	// First pass: render each frame, populate cache, write to stdout.
	for i, img := range scaled {
		if i > 0 {
			fmt.Printf("\033[%dA", blockH)
		}
		var buf bytes.Buffer
		if monochrome {
			octant.RenderMono(img, &buf)
		} else {
			octant.Render(img, &buf)
		}
		cache[i] = buf.Bytes()
		os.Stdout.Write(cache[i])
		if !wait(i) {
			return
		}
	}

	if g.LoopCount < 0 {
		return
	}

	// Subsequent loops: write cached bytes directly.
	for {
		fmt.Printf("\033[%dA", blockH)
		select {
		case <-sigs:
			return
		default:
		}
		for i, frame := range cache {
			if i > 0 {
				fmt.Printf("\033[%dA", blockH)
			}
			os.Stdout.Write(frame)
			if !wait(i) {
				return
			}
		}
	}
}
