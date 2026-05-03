package main

import (
	"fmt"
	"io"

	"github.com/reynoldsme/octant/oscilloscope"
)

// drawOverlay writes a three-line status + keybinding overlay anchored to the
// bottom-left corner of the terminal frame using ANSI cursor positioning.
// The text is dim so it doesn't compete with the phosphor trace.
func drawOverlay(w io.Writer, cfg oscilloscope.Config, rows int) {
	if rows < 4 {
		return
	}

	onOff := func(v bool) string {
		if v {
			return "on "
		}
		return "off"
	}
	gridLabel := [3]string{"black", "color", "off  "}

	// Line 1: display controls.
	line1 := fmt.Sprintf(
		" \x1b[2;37m+/- gain:%-5.2f   e/E intensity:%+.2f   h/H hue:%-3d   p/P persist:%+.1f\x1b[0m",
		cfg.MainGain, cfg.ExposureStops, int(cfg.Hue), cfg.Persistence,
	)

	// Line 2: sweep controls.
	line2 := fmt.Sprintf(
		" \x1b[2;37ms sweep:%s   a auto:%s   t/T trig:%+.2f   m/M ms/div:%.2f\x1b[0m",
		onOff(cfg.SweepOn), onOff(cfg.SweepAutoTrigger),
		cfg.SweepTriggerValue, cfg.SweepMsDiv,
	)

	// Line 3: remaining toggles.
	line3 := fmt.Sprintf(
		" \x1b[2;37mg grid:%s   f freeze:%s   r reset   q quit\x1b[0m",
		gridLabel[cfg.GridMode%3], onOff(cfg.FreezeImage),
	)

	fmt.Fprintf(w, "\x1b[%d;1H%s\x1b[%d;1H%s\x1b[%d;1H%s",
		rows-2, line1,
		rows-1, line2,
		rows, line3,
	)
}
