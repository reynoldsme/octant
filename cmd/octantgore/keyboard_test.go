package main

import (
	"testing"
	"time"

	gore "github.com/AndreRenaud/gore"
)

// --- drainCSI ---

func chanFrom(bs ...byte) chan byte {
	ch := make(chan byte, len(bs))
	for _, b := range bs {
		ch <- b
	}
	return ch
}

func TestDrainCSI_ArrowUp(t *testing.T) {
	// ESC already consumed; remaining: [ A
	ch := chanFrom('[', 'A')
	params, final, ok := drainCSI(ch)
	if !ok {
		t.Fatal("expected ok")
	}
	if final != 'A' {
		t.Fatalf("final = %q, want 'A'", final)
	}
	if len(params) != 0 {
		t.Fatalf("params = %q, want empty", params)
	}
}

func TestDrainCSI_KittyU(t *testing.T) {
	// ESC already consumed; remaining: [ 9 7 ; 1 ; 3 u
	ch := chanFrom('[', '9', '7', ';', '1', ';', '3', 'u')
	params, final, ok := drainCSI(ch)
	if !ok {
		t.Fatal("expected ok")
	}
	if final != 'u' {
		t.Fatalf("final = %q, want 'u'", final)
	}
	if string(params) != "97;1;3" {
		t.Fatalf("params = %q, want %q", params, "97;1;3")
	}
}

func TestDrainCSI_NoBracket(t *testing.T) {
	ch := chanFrom('O', 'A') // VT100 SS3 prefix, not CSI
	_, _, ok := drainCSI(ch)
	if ok {
		t.Fatal("expected not ok when '[' missing")
	}
}

func TestDrainCSI_EmptyChannel(t *testing.T) {
	ch := chanFrom() // stalls immediately
	_, _, ok := drainCSI(ch)
	if ok {
		t.Fatal("expected not ok on stall")
	}
}

func TestDrainCSI_StallAfterBracket(t *testing.T) {
	ch := chanFrom('[') // stalls after bracket with no params or final
	_, _, ok := drainCSI(ch)
	if ok {
		t.Fatal("expected not ok on mid-stall")
	}
}

// --- parseKittyCSI ---

func TestParseKittyCSI_ArrowUpPress(t *testing.T) {
	var ev gore.DoomEvent
	// Simple arrow: no params, final 'A' — default event type is press.
	if !parseKittyCSI(nil, 'A', &ev) {
		t.Fatal("expected true")
	}
	if ev.Type != gore.Ev_keydown {
		t.Fatalf("Type = %v, want Ev_keydown", ev.Type)
	}
	if ev.Key != gore.KEY_UPARROW1 {
		t.Fatalf("Key = %v, want KEY_UPARROW1", ev.Key)
	}
}

func TestParseKittyCSI_ArrowUpRelease(t *testing.T) {
	var ev gore.DoomEvent
	// ESC [ 1 ; 1 ; 3 A
	if !parseKittyCSI([]byte("1;1;3"), 'A', &ev) {
		t.Fatal("expected true")
	}
	if ev.Type != gore.Ev_keyup {
		t.Fatalf("Type = %v, want Ev_keyup", ev.Type)
	}
	if ev.Key != gore.KEY_UPARROW1 {
		t.Fatalf("Key = %v, want KEY_UPARROW1", ev.Key)
	}
}

func TestParseKittyCSI_ArrowDownLeftRight(t *testing.T) {
	cases := []struct {
		final byte
		key   uint8
	}{
		{'B', gore.KEY_DOWNARROW1},
		{'C', gore.KEY_RIGHTARROW1},
		{'D', gore.KEY_LEFTARROW1},
	}
	for _, c := range cases {
		var ev gore.DoomEvent
		if !parseKittyCSI(nil, c.final, &ev) {
			t.Fatalf("final %q: expected true", c.final)
		}
		if ev.Key != c.key {
			t.Fatalf("final %q: Key = %v, want %v", c.final, ev.Key, c.key)
		}
		if ev.Type != gore.Ev_keydown {
			t.Fatalf("final %q: Type = %v, want Ev_keydown", c.final, ev.Type)
		}
	}
}

func TestParseKittyCSI_LetterPress(t *testing.T) {
	var ev gore.DoomEvent
	// ESC [ 97 ; 1 ; 1 u  →  'a' press
	if !parseKittyCSI([]byte("97;1;1"), 'u', &ev) {
		t.Fatal("expected true")
	}
	if ev.Type != gore.Ev_keydown {
		t.Fatalf("Type = %v, want Ev_keydown", ev.Type)
	}
	if ev.Key != 'a' {
		t.Fatalf("Key = %q, want 'a'", ev.Key)
	}
}

func TestParseKittyCSI_LetterRepeat(t *testing.T) {
	var ev gore.DoomEvent
	// ESC [ 97 ; 1 ; 2 u  →  'a' repeat (treated as keydown)
	if !parseKittyCSI([]byte("97;1;2"), 'u', &ev) {
		t.Fatal("expected true")
	}
	if ev.Type != gore.Ev_keydown {
		t.Fatalf("Type = %v, want Ev_keydown (repeat counts as down)", ev.Type)
	}
}

func TestParseKittyCSI_LetterRelease(t *testing.T) {
	var ev gore.DoomEvent
	// ESC [ 97 ; 1 ; 3 u  →  'a' release
	if !parseKittyCSI([]byte("97;1;3"), 'u', &ev) {
		t.Fatal("expected true")
	}
	if ev.Type != gore.Ev_keyup {
		t.Fatalf("Type = %v, want Ev_keyup", ev.Type)
	}
	if ev.Key != 'a' {
		t.Fatalf("Key = %q, want 'a'", ev.Key)
	}
}

func TestParseKittyCSI_EscapeRelease(t *testing.T) {
	var ev gore.DoomEvent
	// ESC [ 27 ; 1 ; 3 u
	if !parseKittyCSI([]byte("27;1;3"), 'u', &ev) {
		t.Fatal("expected true")
	}
	if ev.Type != gore.Ev_keyup {
		t.Fatalf("Type = %v, want Ev_keyup", ev.Type)
	}
	if ev.Key != gore.KEY_ESCAPE {
		t.Fatalf("Key = %v, want KEY_ESCAPE", ev.Key)
	}
}

func TestParseKittyCSI_UnknownCode(t *testing.T) {
	var ev gore.DoomEvent
	// Codepoint 9999 — not mapped
	if parseKittyCSI([]byte("9999;1;1"), 'u', &ev) {
		t.Fatal("expected false for unknown code")
	}
}

func TestParseKittyCSI_UnknownFinal(t *testing.T) {
	var ev gore.DoomEvent
	if parseKittyCSI(nil, 'Z', &ev) {
		t.Fatal("expected false for unmapped final byte")
	}
}

// --- mapKittyCode ---

func TestMapKittyCode(t *testing.T) {
	cases := []struct {
		code int
		key  uint8
		ok   bool
	}{
		{27, gore.KEY_ESCAPE, true},
		{13, gore.KEY_ENTER, true},
		{9, gore.KEY_TAB, true},
		{32, gore.KEY_USE1, true},
		{',', gore.KEY_FIRE1, true},
		{'a', 'a', true},
		{'z', 'z', true},
		{'A', 'a', true}, // uppercase → lowercase DOOM key
		{'Z', 'z', true},
		{'0', '0', true},
		{'9', '9', true},
		{1, 0, false},   // unmapped control
		{128, 0, false}, // unmapped high
	}
	for _, c := range cases {
		got, ok := mapKittyCode(c.code)
		if ok != c.ok {
			t.Errorf("mapKittyCode(%d): ok=%v, want %v", c.code, ok, c.ok)
			continue
		}
		if ok && got != c.key {
			t.Errorf("mapKittyCode(%d): key=%v, want %v", c.code, got, c.key)
		}
	}
}

// --- mapBareByte ---

func TestMapBareByte(t *testing.T) {
	cases := []struct {
		b   byte
		key uint8
		ok  bool
	}{
		{'\r', gore.KEY_ENTER, true},
		{'\n', gore.KEY_ENTER, true},
		{'\t', gore.KEY_TAB, true},
		{' ', gore.KEY_USE1, true},
		{',', gore.KEY_FIRE1, true},
		{'a', 'a', true},
		{'z', 'z', true},
		{'A', 'a', true},
		{'0', '0', true},
		{0x01, 0, false}, // SOH
		{0x7f, 0, false}, // DEL
	}
	for _, c := range cases {
		got, ok := mapBareByte(c.b)
		if ok != c.ok {
			t.Errorf("mapBareByte(%q): ok=%v, want %v", c.b, ok, c.ok)
			continue
		}
		if ok && got != c.key {
			t.Errorf("mapBareByte(%q): key=%v, want %v", c.b, got, c.key)
		}
	}
}

// --- mapKey (legacy) ---

func TestMapKey(t *testing.T) {
	cases := []struct {
		seq string
		key uint8
		ok  bool
	}{
		{"\x1b[A", gore.KEY_UPARROW1, true},
		{"\x1b[B", gore.KEY_DOWNARROW1, true},
		{"\x1b[C", gore.KEY_RIGHTARROW1, true},
		{"\x1b[D", gore.KEY_LEFTARROW1, true},
		{" ", gore.KEY_USE1, true},
		{"\r", gore.KEY_ENTER, true},
		{"\n", gore.KEY_ENTER, true},
		{"\x1b", gore.KEY_ESCAPE, true},
		{"\t", gore.KEY_TAB, true},
		{",", gore.KEY_FIRE1, true},
		{"w", 'w', true},
		{"W", 'w', true},
		{"5", '5', true},
		{"\x00", 0, false},
	}
	for _, c := range cases {
		got, ok := mapKey([]byte(c.seq))
		if ok != c.ok {
			t.Errorf("mapKey(%q): ok=%v, want %v", c.seq, ok, c.ok)
			continue
		}
		if ok && got != c.key {
			t.Errorf("mapKey(%q): key=%v, want %v", c.seq, got, c.key)
		}
	}
}

// --- GetEvent integration ---

// newTestFrontend creates a minimal octantFrontend wired to a byte channel.
// soundSystem and musicSystem are nil; GetEvent does not touch them.
func newTestFrontend(kitty bool, keys chan byte) *octantFrontend {
	return &octantFrontend{
		keys:            keys,
		kittyEnabled:    kitty,
		outstandingDown: make(map[uint8]time.Time),
	}
}

func TestGetEvent_BareKeyDown_Kitty(t *testing.T) {
	ch := chanFrom('w')
	f := newTestFrontend(true, ch)
	var ev gore.DoomEvent
	if !f.GetEvent(&ev) {
		t.Fatal("expected event")
	}
	if ev.Type != gore.Ev_keydown || ev.Key != 'w' {
		t.Fatalf("got Type=%v Key=%q, want Ev_keydown 'w'", ev.Type, ev.Key)
	}
	if _, pending := f.outstandingDown['w']; !pending {
		t.Fatal("expected outstandingDown entry for synthesised-up fallback")
	}
}

// A real CSI release must cancel the synthesised-up entry so DOOM gets
// exactly one Ev_keyup, not two.
func TestGetEvent_RealReleaseCancelsSynthesised(t *testing.T) {
	ch := make(chan byte, 16)
	// Bare 'w' press
	ch <- 'w'
	f := newTestFrontend(true, ch)

	var ev gore.DoomEvent
	if !f.GetEvent(&ev) || ev.Type != gore.Ev_keydown {
		t.Fatal("expected keydown")
	}
	if _, ok := f.outstandingDown['w']; !ok {
		t.Fatal("expected outstandingDown entry after press")
	}

	// Inject CSI release: ESC [ 119 ; 1 ; 3 u  (119 = 'w')
	for _, b := range []byte("\x1b[119;1;3u") {
		ch <- b
	}
	if !f.GetEvent(&ev) || ev.Type != gore.Ev_keyup || ev.Key != 'w' {
		t.Fatalf("expected keyup for 'w', got Type=%v Key=%q", ev.Type, ev.Key)
	}
	if _, still := f.outstandingDown['w']; still {
		t.Fatal("real release must remove outstandingDown entry (no double keyup)")
	}
}

// A CSI repeat event must reset the synthesised-up timer so it doesn't fire
// prematurely while the key is physically held.
func TestGetEvent_RepeatResetsTimer(t *testing.T) {
	ch := make(chan byte, 16)
	ch <- 'w' // press (bare byte)
	f := newTestFrontend(true, ch)

	var ev gore.DoomEvent
	f.GetEvent(&ev) // consume keydown

	// Record timestamp from initial press.
	ts0 := f.outstandingDown['w']

	// Tiny sleep so the next timestamp is strictly later.
	time.Sleep(2 * time.Millisecond)

	// Inject CSI repeat: ESC [ 119 ; 1 ; 2 u
	for _, b := range []byte("\x1b[119;1;2u") {
		ch <- b
	}
	var ev2 gore.DoomEvent
	if !f.GetEvent(&ev2) || ev2.Type != gore.Ev_keydown {
		t.Fatal("expected keydown for repeat event")
	}
	ts1 := f.outstandingDown['w']
	if !ts1.After(ts0) {
		t.Fatal("repeat must reset outstandingDown timer to a later timestamp")
	}
}

// A synthesised key-up fires in Kitty mode when no real release arrives within
// the 1 s fallback window.
func TestGetEvent_SynthesisedFallback_Kitty(t *testing.T) {
	ch := chanFrom('w')
	f := newTestFrontend(true, ch)

	var ev gore.DoomEvent
	f.GetEvent(&ev) // consume keydown

	// Back-date the timestamp so the 1 s fallback fires immediately.
	f.outstandingDown['w'] = time.Now().Add(-2 * time.Second)

	var ev2 gore.DoomEvent
	if !f.GetEvent(&ev2) {
		t.Fatal("expected synthesised keyup")
	}
	if ev2.Type != gore.Ev_keyup || ev2.Key != 'w' {
		t.Fatalf("got Type=%v Key=%q, want Ev_keyup 'w'", ev2.Type, ev2.Key)
	}
	if _, still := f.outstandingDown['w']; still {
		t.Fatal("synthesised keyup must remove outstandingDown entry")
	}
}

// Arrow press (CSI, no params) → Ev_keydown; CSI release → Ev_keyup.
func TestGetEvent_ArrowPressRelease(t *testing.T) {
	ch := make(chan byte, 16)

	// Arrow up press: ESC [ 1 ; 1 ; 1 A
	for _, b := range []byte("\x1b[1;1;1A") {
		ch <- b
	}
	f := newTestFrontend(true, ch)

	var ev gore.DoomEvent
	if !f.GetEvent(&ev) || ev.Type != gore.Ev_keydown || ev.Key != gore.KEY_UPARROW1 {
		t.Fatalf("expected KEY_UPARROW1 keydown, got Type=%v Key=%v", ev.Type, ev.Key)
	}
	// Arrow should be tracked in outstandingDown as a fallback.
	if _, ok := f.outstandingDown[gore.KEY_UPARROW1]; !ok {
		t.Fatal("expected outstandingDown entry for arrow (fallback)")
	}

	// Arrow up release: ESC [ 1 ; 1 ; 3 A
	for _, b := range []byte("\x1b[1;1;3A") {
		ch <- b
	}
	if !f.GetEvent(&ev) || ev.Type != gore.Ev_keyup || ev.Key != gore.KEY_UPARROW1 {
		t.Fatalf("expected KEY_UPARROW1 keyup, got Type=%v Key=%v", ev.Type, ev.Key)
	}
	if _, still := f.outstandingDown[gore.KEY_UPARROW1]; still {
		t.Fatal("arrow release must remove outstandingDown entry")
	}
}

// With kittyReportAll (flag 8), the terminal sends ALL keys as CSI u — no bare
// bytes. Press arrives as event-type 1, release as event-type 3.
func TestGetEvent_CSIPressAndRelease(t *testing.T) {
	ch := make(chan byte, 32)
	// 'w' press: ESC [ 119 ; 1 ; 1 u
	for _, b := range []byte("\x1b[119;1;1u") {
		ch <- b
	}
	f := newTestFrontend(true, ch)

	var ev gore.DoomEvent
	if !f.GetEvent(&ev) || ev.Type != gore.Ev_keydown || ev.Key != 'w' {
		t.Fatalf("expected keydown 'w', got Type=%v Key=%q", ev.Type, ev.Key)
	}
	if _, ok := f.outstandingDown['w']; !ok {
		t.Fatal("expected outstandingDown entry after CSI press")
	}

	// 'w' release: ESC [ 119 ; 1 ; 3 u
	for _, b := range []byte("\x1b[119;1;3u") {
		ch <- b
	}
	if !f.GetEvent(&ev) || ev.Type != gore.Ev_keyup || ev.Key != 'w' {
		t.Fatalf("expected keyup 'w', got Type=%v Key=%q", ev.Type, ev.Key)
	}
	if _, still := f.outstandingDown['w']; still {
		t.Fatal("CSI release must remove outstandingDown (no synthesised double-up)")
	}
}

// In legacy mode the synthesised fallback fires after the shorter 60 ms window.
func TestGetEvent_LegacySynthesisedFallback(t *testing.T) {
	ch := chanFrom('w')
	f := newTestFrontend(false, ch)

	var ev gore.DoomEvent
	f.GetEvent(&ev) // consume keydown

	// Back-date timestamp beyond the 60 ms legacy threshold.
	f.outstandingDown['w'] = time.Now().Add(-200 * time.Millisecond)

	var ev2 gore.DoomEvent
	if !f.GetEvent(&ev2) || ev2.Type != gore.Ev_keyup {
		t.Fatal("expected synthesised keyup in legacy mode")
	}
}
