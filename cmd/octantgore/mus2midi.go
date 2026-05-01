package main

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// toMIDI returns data as standard MIDI bytes.
// If data starts with "MThd" it is already MIDI and is returned unchanged.
// Otherwise it is treated as a DOOM MUS lump and converted.
func toMIDI(data []byte) ([]byte, error) {
	if len(data) >= 4 && string(data[:4]) == "MThd" {
		return data, nil
	}
	return musToMIDI(data)
}

// musToMIDI converts a DOOM MUS lump to a single-track Format-0 MIDI file.
//
// MUS format:
//
//	[0-3]  "MUS\x1a"
//	[4-5]  score length    (uint16 LE, unused)
//	[6-7]  score start     (uint16 LE, offset from start of data)
//	[8-9]  primary channels
//	[10-11] secondary channels
//	[12-13] instrument count
//	[14-15] padding
//	[16…]  instrument list  (instrument_count × 2 bytes)
//	[@score_start] event stream
//
// Event descriptor byte:
//
//	bit 7:   "last" — a VLQ delay follows after this event
//	bits 6-4: event type (0-6)
//	bits 3-0: channel (0-15; 15 = percussion → MIDI channel 9)
//
// MIDI output uses division=70 and tempo=500000 µs/quarter so that
// 1 MUS tick == 1 MIDI tick (both at 140 Hz).
func musToMIDI(mus []byte) ([]byte, error) {
	if len(mus) < 16 || string(mus[:4]) != "MUS\x1a" {
		return nil, errors.New("not a MUS file")
	}
	scoreStart := int(binary.LittleEndian.Uint16(mus[6:8]))
	if scoreStart >= len(mus) {
		return nil, errors.New("score start out of range")
	}

	var track bytes.Buffer

	// Tempo meta-event: 500 000 µs/quarter → 70 ticks/quarter = 140 ticks/s
	track.Write([]byte{0x00, 0xFF, 0x51, 0x03, 0x07, 0xA1, 0x20})

	var chanVol [16]byte
	for i := range chanVol {
		chanVol[i] = 127
	}

	// pendingDelta carries the MUS group delay forward and becomes the
	// delta-time prefix of the next MIDI event we emit.  Writing the delay
	// inline after the current event (then writing 0 before the next) would
	// insert a spurious extra VLQ that corrupts the MIDI stream.
	var pendingDelta int

	pos := scoreStart
loop:
	for pos < len(mus) {
		ev := mus[pos]
		pos++

		last := ev&0x80 != 0
		evType := (ev & 0x70) >> 4
		musCh := int(ev & 0x0F)
		midiCh := musMIDIChannel(musCh)

		switch evType {
		case 0: // release note
			if pos >= len(mus) {
				return nil, errors.New("truncated at release-note")
			}
			note := mus[pos] & 0x7F
			pos++
			writeVLQ(&track, pendingDelta)
			pendingDelta = 0
			track.Write([]byte{0x80 | byte(midiCh), note, 0})

		case 1: // play note
			if pos >= len(mus) {
				return nil, errors.New("truncated at play-note")
			}
			b := mus[pos]
			pos++
			note := b & 0x7F
			if b&0x80 != 0 {
				if pos >= len(mus) {
					return nil, errors.New("truncated at note volume")
				}
				chanVol[musCh] = mus[pos] & 0x7F
				pos++
			}
			writeVLQ(&track, pendingDelta)
			pendingDelta = 0
			track.Write([]byte{0x90 | byte(midiCh), note, chanVol[musCh]})

		case 2: // pitch wheel
			if pos >= len(mus) {
				return nil, errors.New("truncated at pitch wheel")
			}
			// MUS: 0-255, center=128.  MIDI: 0-16383, center=8192.
			// 128*64 = 8192 ✓;  255*64 = 16320 ≈ max.
			bend := int(mus[pos]) * 64
			pos++
			if bend > 16383 {
				bend = 16383
			}
			writeVLQ(&track, pendingDelta)
			pendingDelta = 0
			track.Write([]byte{0xE0 | byte(midiCh), byte(bend & 0x7F), byte(bend >> 7)})

		case 3: // system event
			if pos >= len(mus) {
				return nil, errors.New("truncated at system event")
			}
			ctrl := int(mus[pos])
			pos++
			if cc, ok := musSystemCC(ctrl); ok {
				writeVLQ(&track, pendingDelta)
				pendingDelta = 0
				track.Write([]byte{0xB0 | byte(midiCh), byte(cc), 0})
			}

		case 4: // change controller / program change
			if pos+1 >= len(mus) {
				return nil, errors.New("truncated at controller change")
			}
			ctrl := int(mus[pos])
			val := mus[pos+1] & 0x7F
			pos += 2
			if ctrl == 0 {
				writeVLQ(&track, pendingDelta)
				pendingDelta = 0
				track.Write([]byte{0xC0 | byte(midiCh), val})
			} else if cc, ok := musControllerCC(ctrl); ok {
				writeVLQ(&track, pendingDelta)
				pendingDelta = 0
				track.Write([]byte{0xB0 | byte(midiCh), byte(cc), val})
			}

		case 6: // score end
			break loop
		}

		if last {
			delay, newPos := readMUSDelay(mus, pos)
			pos = newPos
			pendingDelta += delay
		}
	}

	// End of track
	track.Write([]byte{0x00, 0xFF, 0x2F, 0x00})

	td := track.Bytes()
	var out bytes.Buffer
	out.WriteString("MThd")
	binary.Write(&out, binary.BigEndian, uint32(6))  // chunk length
	binary.Write(&out, binary.BigEndian, uint16(0))  // format 0
	binary.Write(&out, binary.BigEndian, uint16(1))  // 1 track
	binary.Write(&out, binary.BigEndian, uint16(70)) // ticks per quarter
	out.WriteString("MTrk")
	binary.Write(&out, binary.BigEndian, uint32(len(td)))
	out.Write(td)
	return out.Bytes(), nil
}

// musMIDIChannel maps a MUS channel number to a MIDI channel number.
// MUS channel 15 (percussion) maps to MIDI channel 9; channels 9-14
// shift up by one to leave MIDI channel 9 free for percussion.
func musMIDIChannel(ch int) int {
	if ch == 15 {
		return 9
	}
	if ch < 9 {
		return ch
	}
	return ch + 1
}

// musSystemCC maps MUS system-event controller numbers (10-14) to MIDI CCs.
var musSystemCCTable = [15]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 120, 123, 126, 127, 121}

func musSystemCC(ctrl int) (int, bool) {
	if ctrl < 10 || ctrl >= len(musSystemCCTable) {
		return 0, false
	}
	return musSystemCCTable[ctrl], true
}

// musControllerCC maps MUS change-controller numbers to MIDI CC numbers.
// Controller 0 is a program change and must be handled separately.
var musControllerCCTable = [15]int{0, 0, 1, 7, 10, 11, 91, 93, 64, 67, 120, 123, 126, 127, 121}

func musControllerCC(ctrl int) (int, bool) {
	if ctrl <= 0 || ctrl >= len(musControllerCCTable) {
		return 0, false
	}
	return musControllerCCTable[ctrl], true
}

// readMUSDelay reads a variable-length quantity from mus at pos.
func readMUSDelay(mus []byte, pos int) (delay, newPos int) {
	for pos < len(mus) {
		b := mus[pos]
		pos++
		delay = (delay << 7) | int(b&0x7F)
		if b&0x80 == 0 {
			break
		}
	}
	return delay, pos
}

// writeVLQ writes a MIDI variable-length quantity to w.
func writeVLQ(w *bytes.Buffer, value int) {
	var buf [4]byte
	n := 0
	buf[n] = byte(value & 0x7F)
	n++
	value >>= 7
	for value > 0 {
		buf[n] = byte(value&0x7F) | 0x80
		n++
		value >>= 7
	}
	for i := n - 1; i >= 0; i-- {
		w.WriteByte(buf[i])
	}
}
