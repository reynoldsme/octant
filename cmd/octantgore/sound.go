package main

import (
	"bytes"
	"encoding/binary"
	"sync"

	"github.com/ebitengine/oto/v3"
)

const (
	outputSampleRate = 44100
	doomMaxVol       = 127
	audioScale       = 0.5
	numDoomChannels  = 8
)

// soundSystem implements CacheSound and PlaySound for the gore.DoomFrontend
// interface using the oto audio library.
//
// DOOM sound lumps are 8-bit unsigned mono PCM at (usually) 11025 Hz wrapped
// in a DMX header. convertDoomPCM upsamples them to 16-bit stereo at 44100 Hz
// for oto, which requires the higher rate on most platforms.
type soundSystem struct {
	ctx     *oto.Context
	mu      sync.Mutex
	cache   map[string][]byte
	players [numDoomChannels]*oto.Player
}

// newAudioContext creates the shared oto context used by both sound effects
// and music. Returns nil if audio hardware is unavailable.
func newAudioContext() *oto.Context {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   outputSampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return nil
	}
	<-ready
	return ctx
}

func newSoundSystem(ctx *oto.Context) *soundSystem {
	return &soundSystem{ctx: ctx, cache: make(map[string][]byte)}
}

// parseDoomSFX extracts the sample rate and raw PCM bytes from a DMX sound lump.
//
// DMX format:
//   - bytes 0–1: format identifier (little-endian uint16, must be 3)
//   - bytes 2–3: sample rate (little-endian uint16)
//   - bytes 4–7: sample count (little-endian uint32)
//   - bytes 8–23: 16 bytes of DMX padding (skipped)
//   - bytes 24–(24+count-17): PCM data (8-bit unsigned, 128 = silence)
//   - last 16 bytes: trailing DMX padding (trimmed)
func parseDoomSFX(data []byte) (sampleRate int, pcm []byte, ok bool) {
	if len(data) < 8 || data[0] != 0x03 || data[1] != 0x00 {
		return 0, nil, false
	}
	sr := int(binary.LittleEndian.Uint16(data[2:4]))
	length := int(binary.LittleEndian.Uint32(data[4:8]))
	// Reject if the header's length claim exceeds the lump, or if the sound
	// is too short to survive trimming (matches Chocolate Doom behaviour).
	if length > len(data)-8 || length <= 48 {
		return 0, nil, false
	}
	// Skip 16 bytes of leading DMX padding; trim 16 bytes from the tail.
	start := 24
	end := start + length - 32
	if end > len(data) || start >= end {
		return 0, nil, false
	}
	return sr, data[start:end], true
}

// convertDoomPCM upsamples DOOM's mono 8-bit unsigned PCM to stereo 16-bit LE
// at outputSampleRate using nearest-neighbour (zero-order hold).
func convertDoomPCM(sampleRate int, pcm []byte) []byte {
	ratio := outputSampleRate / sampleRate
	if ratio < 1 {
		ratio = 1
	}
	out := make([]byte, len(pcm)*ratio*4) // ratio × (2 ch × 2 bytes)
	for i, s := range pcm {
		v := int16((int(s) - 128) * 256) // 8-bit unsigned → 16-bit signed
		lo, hi := byte(v), byte(uint16(v)>>8)
		base := i * ratio * 4
		for r := range ratio {
			off := base + r*4
			out[off+0] = lo // left lo
			out[off+1] = hi // left hi
			out[off+2] = lo // right lo
			out[off+3] = hi // right hi
		}
	}
	return out
}

// CacheSound implements gore.DoomFrontend.
func (s *soundSystem) CacheSound(name string, data []byte) {
	sr, pcm, ok := parseDoomSFX(data)
	if !ok {
		return
	}
	converted := convertDoomPCM(sr, pcm)
	s.mu.Lock()
	s.cache[name] = converted
	s.mu.Unlock()
}

// PlaySound implements gore.DoomFrontend.
// channel selects which DOOM sound channel (0–7) to use; starting a sound on
// a busy channel interrupts the previous sound on that channel.
// vol is 0–127 and sep is the stereo pan (ignored; sounds play centred).
func (s *soundSystem) PlaySound(name string, channel, vol, sep int) {
	if s.ctx == nil {
		return
	}
	s.mu.Lock()
	data, ok := s.cache[name]
	s.mu.Unlock()
	if !ok {
		return
	}

	volume := float64(vol) / float64(doomMaxVol) * audioScale
	if volume > 1.0 {
		volume = 1.0
	}

	ch := channel
	if ch < 0 {
		ch = 0
	} else if ch >= numDoomChannels {
		ch = numDoomChannels - 1
	}

	p := s.ctx.NewPlayer(bytes.NewReader(data))
	p.SetVolume(volume)

	s.mu.Lock()
	if s.players[ch] != nil {
		s.players[ch].Close()
	}
	s.players[ch] = p
	s.mu.Unlock()

	p.Play()
}

func (s *soundSystem) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.players {
		if p != nil {
			p.Close()
			s.players[i] = nil
		}
	}
}
