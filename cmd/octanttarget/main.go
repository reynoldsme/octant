// Command target displays an image that can be repositioned and scaled via
// keyboard or HTTP API.
//
// Keyboard controls:
//
//	Arrow keys    Move image (default 5% per keypress)
//	+/=           Scale up (default 10% per keypress)
//	-             Scale down
//	q / Escape    Quit
//
// HTTP API (JSON, default port 8077):
//
//	POST /move    {"x":50,"y":50}          absolute position (0–100%)
//	              {"dx":5,"dy":-5}         relative position delta
//	POST /scale   {"scale":1.5}            absolute scale factor
//	              {"dscale":0.1}           relative scale delta
//	POST /image   {"file":"path/to/img"}   change displayed image
//	GET  /status  returns current state
package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/reynoldsme/octant"
	"golang.org/x/term"
)

//go:embed target.png
var embeddedImg []byte

// state holds the mutable display parameters, protected by a mutex.
// All fields must be accessed with mu held.
type state struct {
	// using a channel-based update pattern so the render loop owns state reads
	// without locking on every frame; HTTP handlers send updates via updateCh.
	updateCh chan func(*stateData)
	queryCh  chan chan stateData
}

type stateData struct {
	imageFile string
	orig      image.Image
	x         float64 // horizontal position 0–100 (0 = left, 100 = right)
	y         float64 // vertical position 0–100 (0 = top, 100 = bottom)
	scale     float64 // scale factor relative to fit-to-terminal size
}

func (d *stateData) clamp() {
	if d.x < 0 {
		d.x = 0
	}
	if d.x > 100 {
		d.x = 100
	}
	if d.y < 0 {
		d.y = 0
	}
	if d.y > 100 {
		d.y = 100
	}
	if d.scale < 0.05 {
		d.scale = 0.05
	}
	if d.scale > 20 {
		d.scale = 20
	}
}

func newState() *state {
	return &state{
		updateCh: make(chan func(*stateData), 32),
		queryCh:  make(chan chan stateData, 8),
	}
}

// update sends a mutation function to the render goroutine.
func (s *state) update(fn func(*stateData)) {
	s.updateCh <- fn
}

// query returns a snapshot of the current state (called from HTTP handlers).
func (s *state) query() stateData {
	ch := make(chan stateData, 1)
	s.queryCh <- ch
	return <-ch
}

// loadImage opens and decodes an image file, returning the decoded image.
func loadImage(file string) (image.Image, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return img, nil
}

// resizeNearest resizes src to newW×newH using nearest-neighbour interpolation.
func resizeNearest(src image.Image, newW, newH int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	for y := 0; y < newH; y++ {
		sy := b.Min.Y + y*srcH/newH
		for x := 0; x < newW; x++ {
			sx := b.Min.X + x*srcW/newW
			r, g, bl, a := src.At(sx, sy).RGBA()
			dst.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)})
		}
	}
	return dst
}

// renderFrame composites the image onto a full-terminal canvas and draws it.
// It also drains any pending updates and query requests.
func renderFrame(t *octant.Terminal, d *stateData) {
	fd := int(os.Stdout.Fd())
	termW, termH, err := term.GetSize(fd)
	if err != nil || termW <= 0 || termH <= 0 {
		return
	}
	// Leave one row so the final newline doesn't cause the terminal to scroll.
	if termH > 1 {
		termH--
	}

	canvasW := termW * 2 // each terminal column = 2 pixels
	canvasH := termH * 4 // each terminal row    = 4 pixels

	canvas := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)

	if d.orig != nil {
		srcB := d.orig.Bounds()
		srcW := float64(srcB.Dx())
		srcH := float64(srcB.Dy())

		// Base size: largest fit within canvas preserving aspect ratio.
		aspectSrc := srcW / srcH
		aspectCanvas := float64(canvasW) / float64(canvasH)
		var baseW, baseH float64
		if aspectSrc > aspectCanvas {
			baseW = float64(canvasW)
			baseH = float64(canvasW) / aspectSrc
		} else {
			baseH = float64(canvasH)
			baseW = float64(canvasH) * aspectSrc
		}

		dispW := int(baseW * d.scale)
		dispH := int(baseH * d.scale)
		if dispW < 1 {
			dispW = 1
		}
		if dispH < 1 {
			dispH = 1
		}

		// Map percentage position to pixel offset.
		// When the image is larger than the canvas, clamped to 0 so draw clips it.
		maxX := canvasW - dispW
		maxY := canvasH - dispH
		px := int(d.x / 100.0 * float64(maxX))
		py := int(d.y / 100.0 * float64(maxY))

		scaled := resizeNearest(d.orig, dispW, dispH)
		dst := image.Rect(px, py, px+dispW, py+dispH)
		draw.Draw(canvas, dst, scaled, image.Point{}, draw.Over)
	}

	t.DrawFrame(canvas)
}

// keyReader spawns a goroutine that reads raw bytes from r into a channel.
func keyReader(r io.Reader) <-chan byte {
	ch := make(chan byte, 128)
	br := bufio.NewReader(r)
	go func() {
		defer close(ch)
		for {
			b, err := br.ReadByte()
			if err != nil {
				return
			}
			ch <- b
		}
	}()
	return ch
}

type keyResult int

const (
	keyNone keyResult = iota
	keyHandled
	keyQuit
)

// handleKey applies a key sequence to the state, returning the action taken.
func handleKey(d *stateData, seq []byte, step, scaleStep float64) keyResult {
	switch string(seq) {
	case "\x1b[A": // up arrow
		d.y -= step
	case "\x1b[B": // down arrow
		d.y += step
	case "\x1b[C": // right arrow
		d.x += step
	case "\x1b[D": // left arrow
		d.x -= step
	case "+", "=":
		d.scale += scaleStep
	case "-":
		d.scale -= scaleStep
	case "q", "Q", "\x1b", "\x03": // q, Q, ESC, Ctrl-C
		return keyQuit
	default:
		return keyNone
	}
	d.clamp()
	return keyHandled
}

// HTTP request/response types.

type moveRequest struct {
	X  *float64 `json:"x"`
	Y  *float64 `json:"y"`
	DX *float64 `json:"dx"`
	DY *float64 `json:"dy"`
}

type scaleRequest struct {
	Scale  *float64 `json:"scale"`
	DScale *float64 `json:"dscale"`
}

type imageRequest struct {
	File string `json:"file"`
}

type statusResponse struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Scale float64 `json:"scale"`
	File  string  `json:"file"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func startHTTPServer(st *state, port int) {
	mux := http.NewServeMux()

	mux.HandleFunc("/move", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var req moveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st.update(func(d *stateData) {
			if req.X != nil {
				d.x = *req.X
			}
			if req.Y != nil {
				d.y = *req.Y
			}
			if req.DX != nil {
				d.x += *req.DX
			}
			if req.DY != nil {
				d.y += *req.DY
			}
			d.clamp()
		})
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/scale", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var req scaleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st.update(func(d *stateData) {
			if req.Scale != nil {
				d.scale = *req.Scale
			}
			if req.DScale != nil {
				d.scale += *req.DScale
			}
			d.clamp()
		})
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/image", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var req imageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		img, err := loadImage(req.File)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st.update(func(d *stateData) {
			d.imageFile = req.File
			d.orig = img
		})
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		snap := st.query()
		writeJSON(w, statusResponse{
			X:     snap.x,
			Y:     snap.y,
			Scale: snap.scale,
			File:  snap.imageFile,
		})
	})

	addr := fmt.Sprintf(":%d", port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("HTTP server error: %v", err)
	}
}

func main() {
	imageFile := flag.String("image", "", "image file to display (defaults to embedded target.png)")
	port := flag.Int("port", 8077, "HTTP API port")
	step := flag.Float64("step", 5, "keyboard movement step (percentage points)")
	scaleStep := flag.Float64("scale-step", 0.1, "keyboard scale step")
	flag.Parse()

	var img image.Image
	var imgName string
	var err error
	if *imageFile != "" {
		img, err = loadImage(*imageFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "target: %v\n", err)
			os.Exit(1)
		}
		imgName = *imageFile
	} else {
		img, _, err = image.Decode(bytes.NewReader(embeddedImg))
		if err != nil {
			fmt.Fprintf(os.Stderr, "target: decode embedded image: %v\n", err)
			os.Exit(1)
		}
		imgName = "target.png"
	}

	d := stateData{
		imageFile: imgName,
		orig:      img,
		x:         50,
		y:         50,
		scale:     1.0,
	}

	st := newState()

	// Switch stdin to raw mode.
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "target: raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(fd, oldState)

	// Clear screen, move cursor home, hide cursor.
	fmt.Print("\x1b[2J\x1b[H\x1b[?25l")
	defer fmt.Print("\x1b[0m\x1b[?25h\r\n")

	go startHTTPServer(st, *port)

	keys := keyReader(os.Stdin)
	t := &octant.Terminal{W: os.Stdout}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()

	renderFrame(t, &d)

	for {
		select {
		case <-sigs:
			return

		case fn := <-st.updateCh:
			fn(&d)
			renderFrame(t, &d)

		case ch := <-st.queryCh:
			ch <- d

		case <-ticker.C:
			// Drain any pending updates before rendering.
			for {
				select {
				case fn := <-st.updateCh:
					fn(&d)
				default:
					goto doneUpdates
				}
			}
		doneUpdates:
			renderFrame(t, &d)

		case b, ok := <-keys:
			if !ok {
				return
			}
			seq := []byte{b}
			if b == 0x1b {
				// Collect escape sequence bytes (e.g. ESC [ A for up arrow).
				select {
				case b2 := <-keys:
					seq = append(seq, b2)
					select {
					case b3 := <-keys:
						seq = append(seq, b3)
					default:
					}
				default:
				}
			}
			switch handleKey(&d, seq, *step, *scaleStep) {
			case keyQuit:
				return
			case keyHandled:
				renderFrame(t, &d)
			}
		}
	}
}
