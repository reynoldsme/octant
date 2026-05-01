// Package kitty encodes images using the Kitty Graphics Protocol for display
// in terminals that support it (kitty, WezTerm, Ghostty, and others).
//
// Images are PNG-encoded and transmitted inline as base64 chunks in APC
// escape sequences. This is the broadest-compatible path: terminals outside
// kitty itself generally only support the direct/inline transmission mode.
package kitty

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
)

// Encoder encodes images using the Kitty Graphics Protocol.
type Encoder struct{}

// maxPayload is the maximum base64 payload size per APC chunk.
const maxPayload = 4096

// Encode writes img to w using the Kitty Graphics Protocol with default settings.
func Encode(w io.Writer, img image.Image) error {
	return (&Encoder{}).Encode(w, img)
}

// Encode encodes img and writes a kitty graphics stream to w.
// The image is PNG-compressed and split into 4096-byte base64 chunks per the
// protocol spec. BestSpeed PNG compression is used to keep per-frame latency low.
func (e *Encoder) Encode(w io.Writer, img image.Image) error {
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil
	}

	var pngBuf bytes.Buffer
	pngEnc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := pngEnc.Encode(&pngBuf, img); err != nil {
		return err
	}

	encoded := make([]byte, base64.StdEncoding.EncodedLen(pngBuf.Len()))
	base64.StdEncoding.Encode(encoded, pngBuf.Bytes())

	bw := bufio.NewWriterSize(w, 1<<16)

	for i := 0; i < len(encoded); i += maxPayload {
		end := i + maxPayload
		if end > len(encoded) {
			end = len(encoded)
		}
		more := 1
		if end == len(encoded) {
			more = 0
		}
		chunk := encoded[i:end]
		if i == 0 {
			// First chunk carries all display parameters:
			//   a=T   transmit and display immediately
			//   f=100 PNG format (dimensions extracted from PNG header)
			//   q=2   suppress all terminal acknowledgement responses
			fmt.Fprintf(bw, "\x1b_Ga=T,f=100,q=2,m=%d;%s\x1b\\", more, chunk)
		} else {
			fmt.Fprintf(bw, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}

	return bw.Flush()
}
