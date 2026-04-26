// Command octantscope is a terminal oscilloscope based on
// https://dood.al/oscilloscope/, rendered using Unicode octant block
// characters with 24-bit ANSI color. An optional --sixel flag uses the
// DEC sixel protocol instead for pixel-exact output.
//
// On startup, an interactive menu lists available audio input devices and
// PulseAudio output monitors (for capturing playback audio). Pass --siggen
// to skip the menu and use the built-in Lissajous signal generator.
//
// Usage:
//
//	octantscope [flags]
//
// Flags:
//
//	--siggen          Skip menu; use the built-in signal generator
//	--gain float      Main gain (-1 to 4, default 1)
//	--exposure float  Exposure stops (-4 to 4, default 2)
//	--hue float       Phosphor hue 0-360 (default 120 = green)
//	--persistence float  Phosphor afterglow (-1 to 1, default 0)
//	--grid int        Grid mode: 0=black (default), 1=colored, 2=off
//	--sweep           Enable time-base sweep mode
//	--sweep-trigger float  Trigger level (-1 to 1, default 0)
//	--sweep-ms-div float   Sweep ms/div (0.25-4, default 1)
//	--swap-xy         Swap X and Y axes
//	--invert-x        Invert X axis
//	--invert-y        Invert Y axis
//	--no-filter       Disable Lanczos upsampling
//	--mono            Monochrome octant output
//	--sixel           Sixel output mode
//	--x-expr string   Signal generator X expression
//	--y-expr string   Signal generator Y expression
//	--a float         Signal generator 'a' parameter (default 3)
//	--a-exp int       Signal generator 'a' exponent 0-3 (default 2 → 300 Hz)
//	--b float         Signal generator 'b' parameter (default 2)
//	--b-exp int       Signal generator 'b' exponent 0-3 (default 2 → 200 Hz)
//	--cols int        Output width in columns (0 = auto-detect)
//
// Keyboard controls (while running):
//
//	q / Ctrl-C   Quit
//	g            Cycle grid: black → colored → off
//	s            Toggle sweep
//	f            Freeze display
//	h / H        Hue -10 / +10
//	p / P        Persistence -0.1 / +0.1
//	+ / -        Gain +0.25 / -0.25
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gordonklaus/portaudio"
	"golang.org/x/term"

	"github.com/reynoldsme/octant"
	"github.com/reynoldsme/octant/oscilloscope"
	"github.com/reynoldsme/octant/sixel"
)

var (
	flagSigGen      = flag.Bool("siggen", false, "use signal generator instead of microphone")
	flagGain        = flag.Float64("gain", 0.0, "main gain exponent (-1 to 4, actual scale = 2^gain)")
	flagExposure    = flag.Float64("exposure", 0.0, "exposure stops (-4 to 4)")
	flagHue         = flag.Float64("hue", 120.0, "phosphor hue 0-360")
	flagPersistence = flag.Float64("persistence", 0.0, "phosphor persistence (-1 to 1)")
	flagGrid        = flag.Int("grid", 0, "grid mode: 0=black, 1=colored, 2=off")
	flagSweep       = flag.Bool("sweep", true, "enable time-base sweep mode")
	flagSweepAuto   = flag.Bool("auto-trigger", false, "auto-retrigger sweep when no edge is found")
	flagSweepTrig   = flag.Float64("sweep-trigger", 0.0, "sweep trigger level (-1 to 1)")
	flagSweepMsDiv  = flag.Float64("sweep-ms-div", 4.0, "sweep ms/div (0.25,0.5,1,2,4,8,16,32)")
	flagSwapXY      = flag.Bool("swap-xy", false, "swap X and Y axes")
	flagInvertX     = flag.Bool("invert-x", false, "invert X axis")
	flagInvertY     = flag.Bool("invert-y", false, "invert Y axis")
	flagNoFilter    = flag.Bool("no-filter", false, "disable Lanczos upsampling")
	flagMono        = flag.Bool("mono", false, "monochrome octant output")
	flagSixel       = flag.Bool("sixel", false, "sixel output mode")
	flagXExpr       = flag.String("x-expr", "sin(2*PI*a*t)*cos(2*PI*b*t)", "signal generator X expression")
	flagYExpr       = flag.String("y-expr", "cos(2*PI*a*t)*cos(2*PI*b*t)", "signal generator Y expression")
	flagA           = flag.Float64("a", 3.0, "signal generator 'a' parameter (0.5-5)")
	flagAExp        = flag.Int("a-exp", 2, "signal generator 'a' exponent 0-3")
	flagB           = flag.Float64("b", 2.0, "signal generator 'b' parameter (0.5-5)")
	flagBExp        = flag.Int("b-exp", 2, "signal generator 'b' exponent 0-3")
	flagCols        = flag.Int("cols", 0, "output width in columns (0 = auto-detect)")
	flagWAV         = flag.String("wav", "", "read audio from WAV file instead of live input")
	flagPNG         = flag.String("png", "", "render final frame to PNG file and exit")
)

const sampleRate = 44100
const framesPerBuffer = 1024

func configFromFlags() oscilloscope.Config {
	return oscilloscope.Config{
		MainGain:          *flagGain,
		ExposureStops:     *flagExposure,
		Hue:               *flagHue,
		Persistence:       *flagPersistence,
		GridMode:          *flagGrid,
		SweepOn:           *flagSweep,
		SweepAutoTrigger:  *flagSweepAuto,
		SweepTriggerValue: *flagSweepTrig,
		SweepMsDiv:        *flagSweepMsDiv,
		SwapXY:            *flagSwapXY,
		InvertX:           *flagInvertX,
		InvertY:           *flagInvertY,
		DisableFilter:     *flagNoFilter,
		SignalGeneratorOn: *flagSigGen,
		XExpression:       *flagXExpr,
		YExpression:       *flagYExpr,
		AValue:            *flagA,
		AExponent:         *flagAExp,
		BValue:            *flagB,
		BExponent:         *flagBExp,
	}
}

func main() {
	flag.Parse()

	// Headless mode: --png with --wav or --siggen; no terminal needed.
	if *flagPNG != "" && (*flagWAV != "" || *flagSigGen) {
		cols := *flagCols
		if cols <= 0 {
			cols = 80
		}
		cfg := configFromFlags()
		scope := oscilloscope.New(cfg, sampleRate)
		scope.Resize(cols*2, cols) // square-ish pixel buffer
		if err := runHeadless(scope, *flagWAV, *flagPNG, *flagSigGen); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "terminal raw mode:", err)
		os.Exit(1)
	}
	defer term.Restore(fd, oldState)
	// Clear screen and restore cursor. Executes before term.Restore (LIFO),
	// so we are still in raw mode — use \r\n, not \n.
	defer fmt.Print("\x1b[0m\x1b[2J\x1b[H\x1b[?25h\r\n")

	fmt.Print("\x1b[2J\x1b[H\x1b[?25l")

	cols, rows, _ := term.GetSize(fd)
	if *flagCols > 0 {
		cols = *flagCols
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	cfg := configFromFlags()
	scope := oscilloscope.New(cfg, sampleRate)
	scope.Resize(cols*2, rows*4)

	// Audio capture channel: non-blocking send drops samples under backpressure.
	sampleCh := make(chan [][2]float64, 16)

	// Shared buffered reader for menu key input and the main key handler.
	// Must be created before showMenu so the menu and render loop use the
	// same underlying buffer (no bytes get lost between the two).
	stdinBR := bufio.NewReader(os.Stdin)

	useSigGen := *flagSigGen

	switch {
	case *flagWAV != "":
		// WAV mode: loop the file into the render loop, no menu or PortAudio needed.
		if err := startWAVPlayback(*flagWAV, sampleCh); err != nil {
			term.Restore(fd, oldState)
			fmt.Fprint(os.Stderr, "\x1b[0m\x1b[2J\x1b[H\x1b[?25h\r\n")
			fmt.Fprintln(os.Stderr, "wav:", err)
			os.Exit(1)
		}

	case !useSigGen:
		// Live audio: initialize PortAudio, show source menu.
		var paErr error
		suppressStderr(func() { paErr = portaudio.Initialize() })
		if paErr != nil {
			term.Restore(fd, oldState)
			fmt.Fprint(os.Stderr, "\x1b[0m\x1b[2J\x1b[H\x1b[?25h\r\n")
			fmt.Fprintln(os.Stderr, "portaudio:", paErr)
			os.Exit(1)
		}
		defer portaudio.Terminate()

		choice := showMenu(stdinBR)
		switch {
		case choice.quit:
			return
		case choice.siggen:
			useSigGen = true
			cfg.SignalGeneratorOn = true
			scope.SetConfig(cfg)
		default:
			stopAudio, err := startAudio(&choice.source, sampleCh)
			if err != nil {
				term.Restore(fd, oldState)
				fmt.Fprint(os.Stderr, "\x1b[0m\x1b[2J\x1b[H\x1b[?25h\r\n")
				fmt.Fprintln(os.Stderr, "audio:", err)
				os.Exit(1)
			}
			defer stopAudio()
		}
	}

	// SIGWINCH: terminal resize.
	winchCh := make(chan os.Signal, 1)
	signal.Notify(winchCh, syscall.SIGWINCH)

	// SIGINT/SIGTERM: clean exit.
	quitCh := make(chan os.Signal, 1)
	signal.Notify(quitCh, os.Interrupt, syscall.SIGTERM)

	keyCh := keyReaderFrom(stdinBR)

	var term_ *octant.Terminal
	if !*flagSixel {
		term_ = &octant.Terminal{W: os.Stdout, Mono: *flagMono}
		if *flagCols > 0 {
			term_.MaxCols = *flagCols
		}
	}
	sixelEnc := &sixel.Encoder{NumColors: 64}

	ticker := time.NewTicker(33 * time.Millisecond) // ~30 fps
	defer ticker.Stop()

	for {
		select {
		case <-quitCh:
			return

		case <-ticker.C:
			// Drain all pending audio batches.
			var all [][2]float64
		drain:
			for {
				select {
				case batch := <-sampleCh:
					all = append(all, batch...)
				default:
					break drain
				}
			}

			frame := scope.Feed(all)
			if frame == nil {
				continue
			}

			if *flagSixel {
				os.Stdout.WriteString("\x1b[H")
				sixelEnc.Encode(os.Stdout, frame)
			} else {
				term_.DrawFrame(frame)
			}
			drawOverlay(os.Stdout, cfg, rows)

		case <-winchCh:
			cols, rows, _ = term.GetSize(fd)
			if *flagCols > 0 {
				cols = *flagCols
			}
			if cols > 0 && rows > 0 {
				scope.Resize(cols*2, rows*4)
			}

		case b, ok := <-keyCh:
			if !ok {
				return
			}
			if handleKey(b, scope, &cfg) {
				return
			}
		}
	}
}

// startAudio opens an audio input stream and forwards samples to out.
// PipeWire sources (input or monitor) are captured via pw-cat; direct ALSA
// devices use PortAudio. portaudio.Initialize must already have been called
// for the direct ALSA path.
// Returns a stop function that terminates the audio source.
func startAudio(src *audioSource, out chan<- [][2]float64) (func(), error) {
	if src != nil && src.pwNodeID > 0 {
		return startPipeWireCapture(src.pwNodeID, src.pwIsMonitor, out)
	}
	if src != nil && src.paDevice != nil {
		return func() {}, openPortAudioStream(src.paDevice, out)
	}
	return func() {}, openPortAudioStream(nil, out)
}

// startPipeWireCapture spawns pw-cat to capture from a PipeWire node by ID.
// For sink monitors, pw-cat targets the sink ID with stream props that route
// to the monitor loopback. stderr is suppressed to keep the terminal clean.
// Returns a stop function that kills the pw-cat process.
func startPipeWireCapture(nodeID int, isMonitor bool, out chan<- [][2]float64) (func(), error) {
	target := fmt.Sprintf("%d", nodeID)
	args := []string{
		"--record",
		"--target", target,
		"--format", "f32",
		"--rate", "44100",
		"--channels", "2",
	}
	if isMonitor {
		// Stream property tells PipeWire to connect this capture stream to the
		// sink's monitor port rather than to a microphone source.
		args = append(args, "--properties", "stream.capture.sink=true")
	}
	args = append(args, "-")

	cmd := exec.Command("pw-cat", args...)
	cmd.Stderr = nil // suppress ALSA/PipeWire noise from reaching the terminal

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pw-cat: %w", err)
	}
	stop := func() {
		cmd.Process.Kill()
		cmd.Wait()
	}
	go func() {
		defer cmd.Wait()
		raw := make([]byte, framesPerBuffer*2*4) // framesPerBuffer × 2ch × 4 bytes/f32
		for {
			if _, err := io.ReadFull(stdout, raw); err != nil {
				return
			}
			batch := make([][2]float64, framesPerBuffer)
			for i := range batch {
				lbits := binary.LittleEndian.Uint32(raw[i*8:])
				rbits := binary.LittleEndian.Uint32(raw[i*8+4:])
				batch[i] = [2]float64{
					float64(math.Float32frombits(lbits)),
					float64(math.Float32frombits(rbits)),
				}
			}
			select {
			case out <- batch:
			default:
			}
		}
	}()
	return stop, nil
}

// openPortAudioStream opens an input stream on dev (nil = system default)
// and forwards samples to out.
func openPortAudioStream(dev *portaudio.DeviceInfo, out chan<- [][2]float64) error {
	var (
		stream   *portaudio.Stream
		in       []float32
		channels int
		err      error
	)

	if dev != nil {
		channels = min(dev.MaxInputChannels, 2)
		p := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   dev,
				Channels: channels,
				Latency:  dev.DefaultLowInputLatency,
			},
			SampleRate:      sampleRate,
			FramesPerBuffer: framesPerBuffer,
		}
		in = make([]float32, framesPerBuffer*channels)
		stream, err = portaudio.OpenStream(p, in)
		if err != nil && channels == 2 {
			// Stereo failed on this device; try mono.
			channels = 1
			p.Input.Channels = 1
			in = make([]float32, framesPerBuffer)
			stream, err = portaudio.OpenStream(p, in)
		}
	} else {
		channels = 2
		in = make([]float32, framesPerBuffer*2)
		stream, err = portaudio.OpenDefaultStream(2, 0, sampleRate, framesPerBuffer, in)
		if err != nil {
			channels = 1
			in = make([]float32, framesPerBuffer)
			stream, err = portaudio.OpenDefaultStream(1, 0, sampleRate, framesPerBuffer, in)
		}
	}
	if err != nil {
		return err
	}

	if channels == 1 {
		go runMonoStream(stream, in, out)
	} else {
		go runStereoStream(stream, in, out)
	}
	return stream.Start()
}

func runStereoStream(stream *portaudio.Stream, in []float32, out chan<- [][2]float64) {
	for {
		if err := stream.Read(); err != nil {
			return
		}
		batch := make([][2]float64, len(in)/2)
		for i := range batch {
			batch[i] = [2]float64{float64(in[i*2]), float64(in[i*2+1])}
		}
		select {
		case out <- batch:
		default:
		}
	}
}

func runMonoStream(stream *portaudio.Stream, in []float32, out chan<- [][2]float64) {
	for {
		if err := stream.Read(); err != nil {
			return
		}
		batch := make([][2]float64, len(in))
		for i, v := range in {
			batch[i] = [2]float64{float64(v), float64(v)}
		}
		select {
		case out <- batch:
		default:
		}
	}
}

// handleKey processes a single keypress and updates cfg/scope.
// Returns true if the program should exit.
func handleKey(b byte, scope *oscilloscope.Scope, cfg *oscilloscope.Config) bool {
	switch b {
	case 'q', 3: // q or Ctrl-C
		return true
	case 'g':
		cfg.GridMode = (cfg.GridMode + 1) % 3
	case 's':
		cfg.SweepOn = !cfg.SweepOn
	case 'a':
		cfg.SweepAutoTrigger = !cfg.SweepAutoTrigger
	case 'f':
		cfg.FreezeImage = !cfg.FreezeImage
	case 'h':
		cfg.Hue = math.Mod(cfg.Hue-10+360, 360)
	case 'H':
		cfg.Hue = math.Mod(cfg.Hue+10, 360)
	case 'e':
		cfg.ExposureStops = clamp(cfg.ExposureStops-0.25, -4, 4)
	case 'E':
		cfg.ExposureStops = clamp(cfg.ExposureStops+0.25, -4, 4)
	case 'p':
		cfg.Persistence = clamp(cfg.Persistence-0.1, -1, 1)
	case 'P':
		cfg.Persistence = clamp(cfg.Persistence+0.1, -1, 1)
	case 't':
		cfg.SweepTriggerValue = clamp(cfg.SweepTriggerValue-0.05, -1, 1)
	case 'T':
		cfg.SweepTriggerValue = clamp(cfg.SweepTriggerValue+0.05, -1, 1)
	case 'm':
		cfg.SweepMsDiv = prevSweepStep(cfg.SweepMsDiv)
	case 'M':
		cfg.SweepMsDiv = nextSweepStep(cfg.SweepMsDiv)
	case '+', '=':
		cfg.MainGain = clamp(cfg.MainGain+0.25, -1, 4)
	case '-':
		cfg.MainGain = clamp(cfg.MainGain-0.25, -1, 4)
	}
	scope.SetConfig(*cfg)
	return false
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

var sweepSteps = []float64{0.25, 0.5, 1, 2, 4, 8, 16, 32}

func prevSweepStep(cur float64) float64 {
	for i := len(sweepSteps) - 1; i > 0; i-- {
		if cur > sweepSteps[i-1]+1e-9 {
			return sweepSteps[i-1]
		}
	}
	return sweepSteps[0]
}

func nextSweepStep(cur float64) float64 {
	for i := 0; i < len(sweepSteps)-1; i++ {
		if cur < sweepSteps[i+1]-1e-9 {
			return sweepSteps[i+1]
		}
	}
	return sweepSteps[len(sweepSteps)-1]
}

// suppressStderr temporarily redirects fd 2 to /dev/null for the duration of
// fn. This silences ALSA/JACK diagnostic noise that PortAudio emits during
// device enumeration; those libraries write directly to the file descriptor
// and cannot be silenced via os.Stderr.
func suppressStderr(fn func()) {
	devNull, err := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		fn()
		return
	}
	defer devNull.Close()
	saved, err := syscall.Dup(2)
	if err != nil {
		fn()
		return
	}
	if syscall.Dup2(int(devNull.Fd()), 2) != nil {
		syscall.Close(saved)
		fn()
		return
	}
	fn()
	syscall.Dup2(saved, 2)
	syscall.Close(saved)
}

// startWAVPlayback loads a WAV file and loops its samples into out at the
// natural playback rate, so the live render loop sees a continuous audio stream.
func startWAVPlayback(wavPath string, out chan<- [][2]float64) error {
	w, err := readWAV(wavPath)
	if err != nil {
		return err
	}
	sr := w.SampleRate
	if sr <= 0 {
		sr = sampleRate
	}
	interval := time.Duration(float64(time.Second) * float64(framesPerBuffer) / float64(sr))
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		pos := 0
		for range ticker.C {
			end := pos + framesPerBuffer
			if end > len(w.Samples) {
				end = len(w.Samples)
			}
			batch := w.Samples[pos:end]
			pos = end % len(w.Samples)
			select {
			case out <- batch:
			default:
			}
		}
	}()
	return nil
}

// runHeadless processes audio (from wavPath or signal generator) through the
// scope and saves the final rendered frame to pngPath. If pngPath is empty the
// frame is discarded (useful for smoke-testing the pipeline).
//
// The scope must already have been resized. Simulates ~3 seconds of frames at
// 30 fps to build up phosphor accumulation before capturing the output frame.
func runHeadless(scope *oscilloscope.Scope, wavPath, pngPath string, useSigGen bool) error {
	const framesTotal = 90 // 3 seconds at 30 fps

	var allSamples [][2]float64

	if wavPath != "" {
		w, err := readWAV(wavPath)
		if err != nil {
			return fmt.Errorf("wav: %w", err)
		}
		allSamples = w.Samples
	}

	// Feed samples in 1024-frame batches, cycling the WAV if shorter than needed.
	batchSize := framesPerBuffer
	var frame *image.RGBA
	pos := 0
	for range framesTotal {
		var batch [][2]float64
		if !useSigGen && len(allSamples) > 0 {
			end := pos + batchSize
			if end > len(allSamples) {
				end = len(allSamples)
			}
			batch = allSamples[pos:end]
			pos = end % len(allSamples)
		}
		frame = scope.Feed(batch)
	}

	if pngPath == "" || frame == nil {
		return nil
	}
	out, err := os.Create(pngPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, frame)
}

// keyReaderFrom spawns a goroutine draining br into a buffered byte channel.
// Using a pre-existing bufio.Reader ensures no bytes buffered during the menu
// are lost when the render loop takes over.
func keyReaderFrom(br *bufio.Reader) <-chan byte {
	ch := make(chan byte, 128)
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
