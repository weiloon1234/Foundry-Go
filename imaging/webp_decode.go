package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/draw"
	"io"

	"golang.org/x/image/vp8l"
	"golang.org/x/image/webp"
)

func decodeWebPSequence(ctx context.Context, c *webpContainer, firstOnly bool) (imageSequence, error) {
	sequence := imageSequence{plays: c.plays}
	canvas := image.NewNRGBA(image.Rect(0, 0, c.width, c.height))
	background := image.NewUniform(c.background)
	draw.Draw(canvas, canvas.Bounds(), background, image.Point{}, draw.Src)
	for _, frame := range c.frames {
		if err := ctx.Err(); err != nil {
			return imageSequence{}, err
		}
		img, err := decodeWebPFrame(ctx, frame)
		if err != nil {
			return imageSequence{}, err
		}
		op := draw.Over
		if frame.noBlend {
			op = draw.Src
		}
		draw.Draw(canvas, frame.bounds, img, img.Bounds().Min, op)
		sequence.delays = append(sequence.delays, frame.delay)
		if firstOnly {
			sequence.frames = []image.Image{canvas}
			return sequence, nil
		}
		sequence.frames = append(sequence.frames, cloneCanvas(canvas))
		if frame.dispose {
			draw.Draw(canvas, frame.bounds, background, image.Point{}, draw.Src)
		}
	}
	return sequence, nil
}

func decodeWebPFrame(ctx context.Context, frame webpFrame) (image.Image, error) {
	var img image.Image
	var err error
	if frame.kind == "VP8L" {
		img, err = vp8l.Decode(bytes.NewReader(frame.bitstream))
	} else {
		// Supply only the validated frame chunks to the existing static codec.
		// Readers borrow compressed input; large ALPH/VP8 payloads are not copied.
		var header [12]byte
		copy(header[:], "RIFF\x00\x00\x00\x00WEBP")
		readers := []io.Reader{bytes.NewReader(header[:])}
		size := uint32(4)
		chunk := func(kind string, body []byte) {
			var h [8]byte
			copy(h[:4], kind)
			binary.LittleEndian.PutUint32(h[4:], uint32(len(body)))
			readers = append(readers, bytes.NewReader(h[:]), bytes.NewReader(body))
			if len(body)&1 != 0 {
				readers = append(readers, bytes.NewReader([]byte{0}))
			}
			size += uint32(8 + len(body) + (len(body) & 1))
		}
		if frame.alpha != nil {
			var extended [10]byte
			extended[0] = 0x10
			putWebPUint24(extended[4:7], uint32(frame.bounds.Dx()-1))
			putWebPUint24(extended[7:10], uint32(frame.bounds.Dy()-1))
			chunk("VP8X", extended[:])
			chunk("ALPH", frame.alpha)
		}
		chunk("VP8 ", frame.bitstream)
		binary.LittleEndian.PutUint32(header[4:], size)
		img, err = webp.Decode(io.MultiReader(readers...))
		if err == nil {
			img, err = webpRGB(ctx, img)
		}
	}
	if err != nil || img == nil || img.Bounds().Size() != frame.bounds.Size() {
		return nil, invalid("WebP frame decoding failed")
	}
	return img, nil
}
