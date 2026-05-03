// Command octantgore runs DOOM in the terminal using octant block characters
// for rendering. Pass the path to a DOOM WAD file as the first argument:
//
//	octantgore -iwad doom.wad
//
// Keyboard controls (same as the termdoom example in the Gore repository):
//
//	Arrow keys  Move / turn
//	,           Fire
//	Space       Use / open
//	Enter       Confirm
//	Escape      Menu / back
//	Tab         Automap
//	0-9         Cheats / menu selection
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AndreRenaud/gore"
	"golang.org/x/term"

	"github.com/reynoldsme/octant"
)

// Kitty keyboard protocol flags.
// https://sw.kovidgoyal.net/kitty/keyboard-protocol/
const (
	kittyDisambiguate = 1 // report formerly-ambiguous keys (ESC, Enter, Tab) as CSI u
	kittyEventTypes   = 2 // include press/repeat/release event type in CSI sequences
	kittyReportAll    = 8 // send ALL keys as CSI u — no bare-byte text for printable keys
)

const (
	kittyPress   = 1
	kittyRepeat  = 2
	kittyRelease = 3
)

// octantFrontend implements gore.DoomFrontend using octant block rendering.
// CacheSound and PlaySound are promoted from the embedded *soundSystem.
// DrawFrame is overridden to poll for music changes before delegating to
// octant.Terminal, which overwrites the previous frame in place via ANSI
// cursor-up sequences.
type octantFrontend struct {
	octant.Terminal
	*soundSystem
	music *musicSystem

	keys            <-chan byte
	kittyEnabled    bool
	outstandingDown map[uint8]time.Time
	debugLog        io.Writer // non-nil when OCTANT_DEBUG_KEYS is set or -debugkeys is used
}

func (f *octantFrontend) dbg(format string, args ...any) {
	if f.debugLog != nil {
		fmt.Fprintf(f.debugLog, format+"\n", args...)
	}
}

func (f *octantFrontend) SetTitle(title string) {
	fmt.Fprintf(f.Terminal.W, "\x1b]0;%s\x07", title)
}

func (f *octantFrontend) DrawFrame(frame *image.RGBA) {
	f.music.poll()

	cols, rows := f.Terminal.TermConstraints()
	scaled := octant.Scale(frame, cols, rows)

	scaledCols := (scaled.Bounds().Dx() + 1) / 2
	leftPad := 0
	if cols > 0 && scaledCols < cols {
		leftPad = (cols - scaledCols) / 2
	}

	var buf bytes.Buffer
	octant.Render(scaled, &buf)
	out := bytes.ReplaceAll(buf.Bytes(), []byte("\n"), []byte("\r\n"))
	out = bytes.TrimSuffix(out, []byte("\r\n"))
	if leftPad > 0 {
		pad := bytes.Repeat([]byte(" "), leftPad)
		sep := append([]byte("\r\n"), pad...)
		lines := bytes.Split(out, []byte("\r\n"))
		out = append(pad, bytes.Join(lines, sep)...)
	}
	fmt.Fprint(f.Terminal.W, "\x1b[H")
	f.Terminal.W.Write(out)
}

// GetEvent polls for the next keyboard event.
//
// In Kitty mode, real key-up events are delivered by the terminal; synth-up
// is disabled entirely. Terminals only auto-repeat the most recently pressed
// key, so a synth-up timeout would incorrectly drop still-held keys whenever
// a second key is pressed alongside them.
// In legacy mode every key uses the synthesised-up fallback with a tight timeout.
func (f *octantFrontend) GetEvent(ev *gore.DoomEvent) bool {
	now := time.Now()

	// Synthesised key-up: legacy mode only. Kitty sends real key-up events, so
	// synth-up must be disabled there — terminals stop auto-repeating the first
	// key when a second key is pressed, which would cause synth-up to fire and
	// drop the still-held first key after 1 second.
	if !f.kittyEnabled {
		for k, ts := range f.outstandingDown {
			if now.Sub(ts) >= 60*time.Millisecond {
				delete(f.outstandingDown, k)
				ev.Type = gore.Ev_keyup
				ev.Key = k
				f.dbg("synth-up key=0x%02x %q (held %v)", k, k, now.Sub(ts).Round(time.Millisecond))
				return true
			}
		}
	}

	// Non-blocking read from the key channel.
	select {
	case b, ok := <-f.keys:
		if !ok {
			return false
		}
		if f.kittyEnabled {
			return f.handleKittyByte(b, ev, now)
		}
		// Legacy: accumulate up to 3-byte ESC sequence non-blocking.
		seq := []byte{b}
		if b == 0x1b {
			select {
			case b2 := <-f.keys:
				seq = append(seq, b2)
				select {
				case b3 := <-f.keys:
					seq = append(seq, b3)
				default:
				}
			default:
			}
		}
		if k, ok := mapKey(seq); ok {
			ev.Type = gore.Ev_keydown
			ev.Key = k
			f.outstandingDown[k] = now
			return true
		}
		return false
	default:
		return false
	}
}

// handleKittyByte dispatches one byte in Kitty keyboard protocol mode.
// With kittyReportAll enabled the terminal sends all keys as CSI sequences; the
// bare-byte path below is a fallback for terminals that honour only a subset of
// the Kitty flags.
func (f *octantFrontend) handleKittyByte(b byte, ev *gore.DoomEvent, now time.Time) bool {
	if b != 0x1b {
		// Bare ASCII — only reached when kittyReportAll is not honoured.
		f.dbg("bare byte=0x%02x %q", b, b)
		k, ok := mapBareByte(b)
		if !ok {
			return false
		}
		ev.Type = gore.Ev_keydown
		ev.Key = k
		f.outstandingDown[k] = now
		return true
	}
	// Start of an escape sequence: drain the rest of the CSI.
	params, final, ok := drainCSI(f.keys)
	if !ok {
		f.dbg("drainCSI failed after ESC")
		return false
	}
	f.dbg("CSI params=%q final=%q", params, final)
	if !parseKittyCSI(params, final, ev) {
		return false
	}
	// Sync outstandingDown with real Kitty events so the synthesised fallback
	// doesn't double-fire. Release cancels the entry; press/repeat resets the
	// timer (extends the window past the terminal's initial-repeat delay).
	if ev.Type == gore.Ev_keyup {
		f.dbg("keyup  key=0x%02x %q (real CSI release)", ev.Key, ev.Key)
		delete(f.outstandingDown, ev.Key)
	} else {
		f.dbg("keydown key=0x%02x %q (CSI press/repeat)", ev.Key, ev.Key)
		f.outstandingDown[ev.Key] = now
	}
	return true
}

// drainCSI reads the remainder of a CSI sequence from ch after the leading ESC
// has been consumed. It expects '[' next, then parameter bytes, then a final
// byte in [0x40, 0x7E]. Non-blocking: returns false if the channel stalls.
func drainCSI(ch <-chan byte) (params []byte, final byte, ok bool) {
	nb := func() (byte, bool) {
		select {
		case b := <-ch:
			return b, true
		default:
			return 0, false
		}
	}

	// First byte after ESC must be '[' (CSI introducer).
	b, got := nb()
	if !got || b != '[' {
		return nil, 0, false
	}

	for {
		b, got = nb()
		if !got {
			return nil, 0, false
		}
		if b >= 0x40 && b <= 0x7E {
			return params, b, true
		}
		params = append(params, b)
	}
}

// parseKittyCSI interprets a CSI sequence under the Kitty keyboard protocol.
// params are the bytes between CSI and final; final is the terminating byte.
func parseKittyCSI(params []byte, final byte, ev *gore.DoomEvent) bool {
	parts := strings.Split(string(params), ";")

	// Modern Kitty encodes event type in the second field as "mods:event_type"
	// (e.g. "1:3" → mods=1, event_type=3). Older CSI-u format used a separate
	// third field ("code;mods;event_type"); support both as a fallback.
	eventType := kittyPress
	if len(parts) >= 2 {
		if sub := strings.SplitN(parts[1], ":", 2); len(sub) == 2 {
			if n, err := strconv.Atoi(sub[1]); err == nil {
				eventType = n
			}
		}
	}
	if eventType == kittyPress && len(parts) >= 3 {
		if n, err := strconv.Atoi(parts[2]); err == nil {
			eventType = n
		}
	}
	isDown := eventType != kittyRelease

	var k uint8
	var mapped bool

	switch final {
	case 'u': // CSI u — Kitty format: code ; mods:event_type u
		code := 0
		if len(parts) >= 1 {
			// First field may include alternate codepoints as "code:alt1:alt2";
			// take only the primary codepoint.
			if n, err := strconv.Atoi(strings.SplitN(parts[0], ":", 2)[0]); err == nil {
				code = n
			}
		}
		k, mapped = mapKittyCode(code)

	case 'A':
		k, mapped = gore.KEY_UPARROW1, true
	case 'B':
		k, mapped = gore.KEY_DOWNARROW1, true
	case 'C':
		k, mapped = gore.KEY_RIGHTARROW1, true
	case 'D':
		k, mapped = gore.KEY_LEFTARROW1, true
	}

	if !mapped {
		return false
	}
	if isDown {
		ev.Type = gore.Ev_keydown
	} else {
		ev.Type = gore.Ev_keyup
	}
	ev.Key = k
	return true
}

// mapKittyCode maps a Unicode codepoint from the Kitty protocol to a DOOM key.
func mapKittyCode(code int) (uint8, bool) {
	switch code {
	case 27:
		return gore.KEY_ESCAPE, true
	case 13:
		return gore.KEY_ENTER, true
	case 9:
		return gore.KEY_TAB, true
	case 32:
		return gore.KEY_USE1, true
	case ',':
		return gore.KEY_FIRE1, true
	}
	if code >= '0' && code <= '9' {
		return uint8(code), true
	}
	if code >= 'A' && code <= 'Z' {
		return uint8(code-'A') + 'a', true
	}
	if code >= 'a' && code <= 'z' {
		return uint8(code), true
	}
	return 0, false
}

// mapBareByte maps a non-ESC ASCII byte to a DOOM key in Kitty mode.
// Kitty still delivers unambiguous printable keys as bare bytes.
func mapBareByte(b byte) (uint8, bool) {
	switch b {
	case '\r', '\n':
		return gore.KEY_ENTER, true
	case '\t':
		return gore.KEY_TAB, true
	case ' ':
		return gore.KEY_USE1, true
	case ',':
		return gore.KEY_FIRE1, true
	}
	if b >= '0' && b <= '9' {
		return b, true
	}
	if b >= 'A' && b <= 'Z' {
		return b - 'A' + 'a', true
	}
	if b >= 'a' && b <= 'z' {
		return b, true
	}
	return 0, false
}

// mapKey translates a raw byte sequence to a DOOM key code (legacy mode).
func mapKey(seq []byte) (uint8, bool) {
	switch string(seq) {
	case "\x1b[A":
		return gore.KEY_UPARROW1, true
	case "\x1b[B":
		return gore.KEY_DOWNARROW1, true
	case "\x1b[C":
		return gore.KEY_RIGHTARROW1, true
	case "\x1b[D":
		return gore.KEY_LEFTARROW1, true
	case " ":
		return gore.KEY_USE1, true
	case "\r", "\n":
		return gore.KEY_ENTER, true
	case "\x1b":
		return gore.KEY_ESCAPE, true
	case "\t":
		return gore.KEY_TAB, true
	case ",":
		return gore.KEY_FIRE1, true
	}
	if len(seq) == 1 {
		b := seq[0]
		if b >= '0' && b <= '9' {
			return b, true
		}
		if b >= 'A' && b <= 'Z' {
			return b - 'A' + 'a', true
		}
		if b >= 'a' && b <= 'z' {
			return b, true
		}
	}
	return 0, false
}

// keyReader spawns a goroutine that continuously reads bytes from br into a
// buffered channel, enabling non-blocking reads in GetEvent.
func keyReader(br *bufio.Reader) <-chan byte {
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

// stdinReady reports whether os.Stdin has data to read within d.
// Uses syscall.Select so it does not consume any bytes.
func stdinReady(d time.Duration) bool {
	fd := int(os.Stdin.Fd())
	var fds syscall.FdSet
	fds.Bits[fd/64] |= 1 << (uint(fd) % 64)
	tv := syscall.NsecToTimeval(d.Nanoseconds())
	n, err := syscall.Select(fd+1, &fds, nil, nil, &tv)
	return n > 0 && err == nil
}

// tryEnableKitty sends the Kitty keyboard protocol query and, if the terminal
// responds, pushes our flag set. Must be called after entering raw mode and
// before starting the keyReader goroutine. Returns true when enabled.
func tryEnableKitty(br *bufio.Reader) bool {
	fmt.Fprint(os.Stdout, "\x1b[?u")

	// Wait up to 100 ms for the first response byte.
	if !stdinReady(100 * time.Millisecond) {
		return false
	}

	// All response bytes arrive atomically; read them from the buffer directly.
	read := func() (byte, bool) {
		b, err := br.ReadByte()
		return b, err == nil
	}

	// Expected response: ESC [ ? {digits} u
	b, ok := read()
	if !ok || b != 0x1b {
		return false
	}
	b, ok = read()
	if !ok || b != '[' {
		return false
	}
	b, ok = read()
	if !ok || b != '?' {
		return false
	}
	for {
		b, ok = read()
		if !ok {
			return false
		}
		if b == 'u' {
			break
		}
		if b < '0' || b > '9' {
			return false
		}
	}

	// Push current flags and enable ours.
	// kittyReportAll (8) tells the terminal to send every key as a CSI u sequence
	// rather than as bare text, which prevents the terminal from sending both a
	// bare byte AND a CSI press event for the same keystroke.
	fmt.Fprintf(os.Stdout, "\x1b[>%du", kittyDisambiguate|kittyEventTypes|kittyReportAll)
	return true
}

func main() {
	// Strip our own flags before gore sees them.
	debugKeysMode, args := extractBoolFlag(os.Args[1:], "debugkeys")
	soundfont, args := extractFlag(args, "soundfont")

	// Validate arguments before touching the terminal: gore.Run does not
	// return an error, so any WAD problem produces garbled output inside the
	// raw/cleared terminal with no way to exit cleanly.
	if err := checkWAD(args); err != nil {
		fmt.Fprintf(os.Stderr, "octantgore: %v\n", err)
		fmt.Fprintln(os.Stderr, "usage: octantgore [-soundfont <file.sf2>] -iwad <doom.wad>")
		os.Exit(1)
	}

	// Open the controlling terminal directly so rendering is unaffected when
	// stdout/stderr are redirected to the log below.
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		tty = os.Stdout // best-effort fallback
	}

	// Switch stdin to raw mode so individual keystrokes arrive immediately
	// without line-buffering or echo.
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "terminal raw mode:", err)
		os.Exit(1)
	}
	defer term.Restore(fd, oldState)

	// Enter alternate screen, clear it, hide cursor; restore on exit.
	fmt.Fprint(tty, "\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l")
	defer fmt.Fprint(tty, "\x1b[0m\x1b[?25h\x1b[?1049l")

	// Detect and enable Kitty keyboard protocol before starting the reader
	// goroutine (both use the same bufio.Reader; only one goroutine may read).
	br := bufio.NewReader(os.Stdin)
	kittyEnabled := tryEnableKitty(br)
	if kittyEnabled {
		defer fmt.Fprint(tty, "\x1b[<1u")
	}

	// Redirect fds 1 and 2 to a log file so that DOOM's diagnostics and C
	// library noise (ALSA, JACK) don't bleed through behind the rendered
	// frames. Dup2 is necessary because C libraries write directly to the raw
	// fds, bypassing Go's os.Stdout/os.Stderr variables.
	if lf, err := os.Create("/tmp/octantgore.log"); err == nil {
		syscall.Dup2(int(lf.Fd()), 1)
		syscall.Dup2(int(lf.Fd()), 2)
		defer lf.Close()
	}

	otoCtx := newAudioContext()
	sound := newSoundSystem(otoCtx)
	defer sound.close()
	music := newMusicSystem(otoCtx, soundfont)
	defer music.close()

	var debugLog io.Writer
	if path := os.Getenv("OCTANT_DEBUG_KEYS"); path != "" {
		if path == "1" {
			path = "/tmp/octantgore-keys.log"
		}
		if lf, err := os.Create(path); err == nil {
			debugLog = lf
			defer lf.Close()
		}
	}
	if debugKeysMode {
		debugLog = rawWriter{tty}
	}

	f := &octantFrontend{
		Terminal:        octant.Terminal{W: tty},
		soundSystem:     sound,
		music:           music,
		keys:            keyReader(br),
		kittyEnabled:    kittyEnabled,
		outstandingDown: make(map[uint8]time.Time),
		debugLog:        debugLog,
	}
	if debugKeysMode {
		runDebugKeys(f)
		return
	}
	gore.Run(f, args)
}

// extractFlag removes -name / --name (with a following value) or
// -name=value / --name=value from args and returns the value and the
// remaining slice. All other flags are passed through untouched so that
// gore can handle them itself.
func extractFlag(args []string, name string) (value string, remaining []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-"+name || a == "--"+name {
			if i+1 < len(args) {
				value = args[i+1]
				i++
			}
			continue
		}
		if v, ok := strings.CutPrefix(a, "-"+name+"="); ok {
			value = v
			continue
		}
		if v, ok := strings.CutPrefix(a, "--"+name+"="); ok {
			value = v
			continue
		}
		remaining = append(remaining, a)
	}
	return value, remaining
}

// rawWriter wraps an io.Writer and converts bare \n to \r\n for raw terminal mode.
type rawWriter struct{ w io.Writer }

func (r rawWriter) Write(p []byte) (int, error) {
	_, err := r.w.Write([]byte(strings.ReplaceAll(string(p), "\n", "\r\n")))
	return len(p), err
}

// extractBoolFlag removes -name / --name from args and returns whether it was
// present along with the remaining slice.
func extractBoolFlag(args []string, name string) (bool, []string) {
	found := false
	var remaining []string
	for _, a := range args {
		if a == "-"+name || a == "--"+name {
			found = true
			continue
		}
		remaining = append(remaining, a)
	}
	return found, remaining
}

// runDebugKeys replaces gore.Run with an interactive key-event log, using the
// same octantFrontend (and therefore the same kitty detection, byte parsing,
// and synthesised-up logic) as the real game.
func runDebugKeys(f *octantFrontend) {
	pr := func(format string, args ...any) {
		fmt.Printf(format+"\r\n", args...)
	}
	pr("octantgore -debugkeys — live keyboard event log")
	pr("────────────────────────────────────────────────")
	if f.kittyEnabled {
		pr("Kitty keyboard protocol: ENABLED")
	} else {
		pr("Kitty keyboard protocol: NOT DETECTED  (running in legacy mode)")
	}
	pr("(dbg lines below show raw bytes; key lines show parsed DoomEvents)")
	pr("Press keys to test. ESC twice or q to quit.")
	pr("")

	escPresses := 0
	for {
		var ev gore.DoomEvent
		for !f.GetEvent(&ev) {
			time.Sleep(1 * time.Millisecond)
		}

		evLabel := "key-DOWN"
		if ev.Type == gore.Ev_keyup {
			evLabel = "key-UP  "
		}
		pr("%s  0x%02x  %q", evLabel, ev.Key, rune(ev.Key))

		if ev.Type == gore.Ev_keydown {
			switch ev.Key {
			case gore.KEY_ESCAPE:
				escPresses++
				if escPresses >= 2 {
					pr("quit.")
					return
				}
			case 'q':
				pr("quit.")
				return
			default:
				escPresses = 0
			}
		}
	}
}

// checkWAD verifies that a -iwad argument is present and the file exists.
func checkWAD(args []string) error {
	for i, arg := range args {
		if arg == "-iwad" {
			if i+1 >= len(args) {
				return fmt.Errorf("-iwad requires a path argument")
			}
			path := args[i+1]
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("WAD file not found: %s", path)
			}
			return nil
		}
	}
	return fmt.Errorf("no WAD file specified (use -iwad <path>)")
}
