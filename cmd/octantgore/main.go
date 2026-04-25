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
	"fmt"
	"io"
	"os"
	"time"

	"github.com/AndreRenaud/gore"
	"golang.org/x/term"

	"github.com/reynoldsme/octant"
)

// octantFrontend implements gore.DoomFrontend using octant block rendering.
// DrawFrame is promoted from the embedded octant.Terminal, which overwrites
// the previous frame in place via ANSI cursor-up sequences.
type octantFrontend struct {
	octant.Terminal

	keys            <-chan byte
	outstandingDown map[uint8]time.Time
}

func (f *octantFrontend) SetTitle(title string) {
	fmt.Fprintf(os.Stdout, "\x1b]0;%s\x07", title)
}

func (f *octantFrontend) CacheSound(name string, data []byte) {}

func (f *octantFrontend) PlaySound(name string, channel, vol, sep int) {}

// GetEvent polls for the next keyboard event, synthesising key-up events
// 60 ms after each key-down (terminals do not send key-up signals).
func (f *octantFrontend) GetEvent(ev *gore.DoomEvent) bool {
	const upDelay = 60 * time.Millisecond
	now := time.Now()

	// Emit any pending key-ups whose delay has elapsed.
	for k, ts := range f.outstandingDown {
		if now.Sub(ts) >= upDelay {
			delete(f.outstandingDown, k)
			ev.Type = gore.Ev_keyup
			ev.Key = k
			return true
		}
	}

	// Non-blocking read from the key channel.
	select {
	case b, ok := <-f.keys:
		if !ok {
			return false
		}
		// Accumulate an escape sequence (e.g. arrow keys: ESC [ A).
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

// mapKey translates a raw byte sequence to a DOOM key code.
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

// keyReader spawns a goroutine that continuously reads bytes from r into a
// buffered channel, enabling non-blocking reads in GetEvent.
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

func main() {
	// Validate arguments before touching the terminal: gore.Run does not
	// return an error, so any WAD problem produces garbled output inside the
	// raw/cleared terminal with no way to exit cleanly.
	args := os.Args[1:]
	if err := checkWAD(args); err != nil {
		fmt.Fprintf(os.Stderr, "octantgore: %v\n", err)
		fmt.Fprintln(os.Stderr, "usage: octantgore -iwad <doom.wad>")
		os.Exit(1)
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

	// Clear screen and hide cursor; restore on exit.
	fmt.Print("\x1b[2J\x1b[H\x1b[?25l")
	defer fmt.Print("\x1b[0m\x1b[?25h")

	f := &octantFrontend{
		Terminal:        octant.Terminal{W: os.Stdout},
		keys:            keyReader(os.Stdin),
		outstandingDown: make(map[uint8]time.Time),
	}
	gore.Run(f, args)
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
