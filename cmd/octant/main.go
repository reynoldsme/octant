// Command octant renders images as Unicode octant-block characters with
// 24-bit ANSI color in the terminal.
//
// Usage:
//
//	octant [flags] image [image …]
//
// Flags:
//
//	-mono        Monochrome (1-bit dithered) output
//	-cols int    Output width in terminal columns (default: auto-detect)
//
// Supported formats: JPEG, PNG, GIF (animated GIFs play in a loop).
// If no image is given, reads from stdin.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/gif"
	"io"
	"os"
	"time"

	"github.com/reynoldsme/octant"
	_ "image/jpeg"
	_ "image/png"
)

func main() {
	mono := flag.Bool("mono", false, "monochrome output")
	cols := flag.Int("cols", 0, "output width in terminal columns (0 = auto)")
	rows := flag.Int("rows", 0, "output height in terminal rows (0 = auto)")
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		files = []string{"-"}
	}

	for _, path := range files {
		if err := render(path, *cols, *rows, *mono); err != nil {
			fmt.Fprintf(os.Stderr, "octant: %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func render(path string, cols, rows int, mono bool) error {
	var r io.Reader
	if path == "-" {
		r = bufio.NewReader(os.Stdin)
	} else {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	isGIF := len(data) >= 6 && (string(data[:6]) == "GIF89a" || string(data[:6]) == "GIF87a")
	if isGIF {
		return renderGIF(data, cols, rows, mono)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	scaled := octant.Scale(img, cols, rows)
	if mono {
		octant.RenderMono(scaled, os.Stdout)
	} else {
		octant.Render(scaled, os.Stdout)
	}
	return nil
}

func renderGIF(data []byte, cols, rows int, mono bool) error {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return err
	}
	frames := octant.ComposeGIFFrames(g)

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	for {
		for i, frame := range frames {
			scaled := octant.Scale(frame, cols, rows)
			fmt.Print("\033[H")
			if mono {
				octant.RenderMono(scaled, os.Stdout)
			} else {
				octant.Render(scaled, os.Stdout)
			}
			delay := 10 * time.Millisecond
			if i < len(g.Delay) && g.Delay[i] > 0 {
				delay = time.Duration(g.Delay[i]) * 10 * time.Millisecond
			}
			time.Sleep(delay)
		}
		if g.LoopCount < 0 {
			break
		}
	}
	return nil
}
