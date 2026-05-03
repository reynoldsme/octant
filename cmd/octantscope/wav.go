package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// wavFile holds the decoded contents of a PCM WAV file.
type wavFile struct {
	Channels   int
	SampleRate int
	Samples    [][2]float64 // stereo; mono files duplicate the channel
}

// readWAV reads a PCM WAV file and returns normalized float64 samples.
// Supports 8, 16, 24, and 32-bit PCM, mono or stereo.
func readWAV(path string) (*wavFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// RIFF header
	var riff [4]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, fmt.Errorf("wav: read RIFF: %w", err)
	}
	if string(riff[:]) != "RIFF" {
		return nil, fmt.Errorf("wav: not a RIFF file")
	}
	var fileSize uint32
	binary.Read(f, binary.LittleEndian, &fileSize)
	var wave [4]byte
	if _, err := io.ReadFull(f, wave[:]); err != nil {
		return nil, fmt.Errorf("wav: read WAVE: %w", err)
	}
	if string(wave[:]) != "WAVE" {
		return nil, fmt.Errorf("wav: not a WAVE file")
	}

	var (
		audioFormat uint16
		channels    uint16
		sampleRate  uint32
		bitsPerSamp uint16
	)
	var dataBytes []byte

	// Scan chunks
	for {
		var id [4]byte
		if _, err := io.ReadFull(f, id[:]); err != nil {
			break
		}
		var size uint32
		if err := binary.Read(f, binary.LittleEndian, &size); err != nil {
			break
		}
		switch string(id[:]) {
		case "fmt ":
			binary.Read(f, binary.LittleEndian, &audioFormat)
			binary.Read(f, binary.LittleEndian, &channels)
			binary.Read(f, binary.LittleEndian, &sampleRate)
			var byteRate uint32
			binary.Read(f, binary.LittleEndian, &byteRate)
			var blockAlign uint16
			binary.Read(f, binary.LittleEndian, &blockAlign)
			binary.Read(f, binary.LittleEndian, &bitsPerSamp)
			if size > 16 {
				io.CopyN(io.Discard, f, int64(size-16))
			}
		case "data":
			dataBytes = make([]byte, size)
			if _, err := io.ReadFull(f, dataBytes); err != nil {
				return nil, fmt.Errorf("wav: read data: %w", err)
			}
		default:
			io.CopyN(io.Discard, f, int64(size))
		}
	}

	if audioFormat != 1 {
		return nil, fmt.Errorf("wav: only PCM (format 1) supported, got %d", audioFormat)
	}
	if channels == 0 || channels > 2 {
		return nil, fmt.Errorf("wav: unsupported channel count %d", channels)
	}
	if dataBytes == nil {
		return nil, fmt.Errorf("wav: no data chunk found")
	}

	bytesPerSample := int(bitsPerSamp) / 8
	frameSize := int(channels) * bytesPerSample
	if frameSize == 0 {
		return nil, fmt.Errorf("wav: invalid frame size")
	}
	nFrames := len(dataBytes) / frameSize

	samples := make([][2]float64, nFrames)
	for i := range samples {
		base := i * frameSize
		left := pcmSample(dataBytes[base:], int(bitsPerSamp))
		var right float64
		if channels == 2 {
			right = pcmSample(dataBytes[base+bytesPerSample:], int(bitsPerSamp))
		} else {
			right = left
		}
		samples[i] = [2]float64{left, right}
	}

	return &wavFile{
		Channels:   int(channels),
		SampleRate: int(sampleRate),
		Samples:    samples,
	}, nil
}

// pcmSample decodes one PCM sample (up to 32-bit) from b, returning [-1, 1].
func pcmSample(b []byte, bits int) float64 {
	switch bits {
	case 8:
		// 8-bit PCM is unsigned (0–255); center is 128.
		return (float64(b[0]) - 128) / 128.0
	case 16:
		v := int16(binary.LittleEndian.Uint16(b[:2]))
		return float64(v) / 32768.0
	case 24:
		// 24-bit little-endian signed.
		raw := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		if raw&0x800000 != 0 {
			raw |= ^0xFFFFFF // sign-extend
		}
		return float64(raw) / 8388608.0
	case 32:
		v := int32(binary.LittleEndian.Uint32(b[:4]))
		return float64(v) / 2147483648.0
	default:
		return 0
	}
}
