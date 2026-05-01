package main

import (
	"bytes"
	"log"
	"os"

	"github.com/AndreRenaud/gore"
	"github.com/ebitengine/oto/v3"
	"github.com/sinshu/go-meltysynth/meltysynth"
)

const renderBlockSamples = 512

// musReader implements io.Reader for oto by pulling samples from a
// MidiFileSequencer on demand. It is only ever called from oto's audio
// goroutine, so no synchronisation is needed on the sequencer itself.
type musReader struct {
	seq    *meltysynth.MidiFileSequencer
	left   []float32
	right  []float32
	pcm    []byte
	pcmPos int
}

func newMusReader(synth *meltysynth.Synthesizer, mf *meltysynth.MidiFile) *musReader {
	seq := meltysynth.NewMidiFileSequencer(synth)
	seq.Play(mf, true) // DOOM music always loops
	return &musReader{
		seq:   seq,
		left:  make([]float32, renderBlockSamples),
		right: make([]float32, renderBlockSamples),
		pcm:   make([]byte, renderBlockSamples*4),
	}
}

func (r *musReader) Read(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		if r.pcmPos < len(r.pcm) {
			n := copy(p[written:], r.pcm[r.pcmPos:])
			r.pcmPos += n
			written += n
			continue
		}
		r.seq.Render(r.left, r.right)
		for i, lf := range r.left {
			l := clampMusicSample(lf)
			rv := clampMusicSample(r.right[i])
			r.pcm[i*4+0] = byte(l)
			r.pcm[i*4+1] = byte(uint16(l) >> 8)
			r.pcm[i*4+2] = byte(rv)
			r.pcm[i*4+3] = byte(uint16(rv) >> 8)
		}
		r.pcmPos = 0
	}
	return written, nil
}

func clampMusicSample(f float32) int16 {
	v := int32(f * 32767)
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// musicSystem detects music changes by polling gore.S_music on every game
// tick (via DrawFrame) and drives a meltysynth synthesizer through oto.
//
// All methods except newMusReader/Read are called from gore's single game-loop
// goroutine. The synthesizer is accessed exclusively from oto's audio goroutine
// while a player is alive; we close the old player before creating a new reader
// to ensure the synthesizer is only ever touched by one goroutine at a time.
type musicSystem struct {
	ctx         *oto.Context
	synthesizer *meltysynth.Synthesizer
	currentName string
	player      *oto.Player
	log         *log.Logger
}

// newMusicSystem loads a SoundFont and prepares the synthesizer.
// Returns a no-op system (synthesizer == nil) on any failure so the
// game continues without music.
func newMusicSystem(ctx *oto.Context, soundfontPath string) *musicSystem {
	lf, err := os.OpenFile("octantgore-music.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		lf = os.Stderr
	}
	logger := log.New(lf, "", log.Ltime|log.Lmicroseconds)

	if ctx == nil || soundfontPath == "" {
		logger.Println("music disabled: no soundfont specified")
		return &musicSystem{log: logger}
	}
	f, err := os.Open(soundfontPath)
	if err != nil {
		logger.Printf("music disabled: %v", err)
		return &musicSystem{log: logger}
	}
	defer f.Close()

	sf, err := meltysynth.NewSoundFont(f)
	if err != nil {
		logger.Printf("music disabled: load soundfont: %v", err)
		return &musicSystem{log: logger}
	}
	settings := meltysynth.NewSynthesizerSettings(int32(outputSampleRate))
	synth, err := meltysynth.NewSynthesizer(sf, settings)
	if err != nil {
		logger.Printf("music disabled: init synthesizer: %v", err)
		return &musicSystem{log: logger}
	}
	logger.Printf("ready: soundfont %q loaded", soundfontPath)
	return &musicSystem{ctx: ctx, synthesizer: synth, log: logger}
}

// poll scans gore.S_music for the active song (the one slot with non-nil
// Fdata) and starts or stops playback as needed. Call once per DrawFrame.
func (s *musicSystem) poll() {
	if s.synthesizer == nil {
		return
	}
	for i := range gore.S_music {
		if gore.S_music[i].Fdata != nil {
			name := gore.S_music[i].Fname
			if name != s.currentName {
				s.log.Printf("song change: slot %d  name=%q  bytes=%d", i, name, len(gore.S_music[i].Fdata))
				s.startSong(name, gore.S_music[i].Fdata)
			}
			return
		}
	}
	if s.currentName != "" {
		s.log.Printf("song stop (was %q)", s.currentName)
		s.stopSong()
	}
}

func (s *musicSystem) startSong(name string, data []byte) {
	format := "MUS"
	if len(data) >= 4 && string(data[:4]) == "MThd" {
		format = "MIDI"
	}
	midi, err := toMIDI(data)
	if err != nil {
		s.log.Printf("  convert error (%s → MIDI): %v", format, err)
		return
	}
	s.log.Printf("  converted %s → %d MIDI bytes", format, len(midi))

	mf, err := meltysynth.NewMidiFile(bytes.NewReader(midi))
	if err != nil {
		s.log.Printf("  parse MIDI error: %v", err)
		return
	}
	s.log.Printf("  MIDI length: %v", mf.GetLength())

	// Stop the old player first so its goroutine is done using the synthesizer
	// before we reset it in newMusReader → seq.Play.
	if s.player != nil {
		s.player.Close()
		s.player = nil
	}

	reader := newMusReader(s.synthesizer, mf)
	p := s.ctx.NewPlayer(reader)
	p.SetVolume(0.5)
	s.player = p
	s.currentName = name
	p.Play()
	s.log.Printf("  playing %q", name)
}

func (s *musicSystem) stopSong() {
	if s.player != nil {
		s.player.Close()
		s.player = nil
	}
	s.currentName = ""
}

func (s *musicSystem) close() {
	s.stopSong()
}
