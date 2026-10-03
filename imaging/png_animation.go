package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/draw"
	"image/png"
)

func decodeAPNG(ctx context.Context, data []byte, l Limits) (imageSequence, error) {
	container, err := parsePNG(data, l)
	if err != nil {
		return imageSequence{}, err
	}
	if !container.animated {
		return imageSequence{}, unsupported()
	}
	sequence := imageSequence{plays: container.plays}
	canvas := image.NewNRGBA(image.Rect(0, 0, int(binary.BigEndian.Uint32(container.header)), int(binary.BigEndian.Uint32(container.header[4:]))))
	for _, frame := range container.frames {
		if err := ctx.Err(); err != nil {
			return imageSequence{}, err
		}
		img, err := decodePNGFrame(ctx, container, frame, int64(len(data)))
		if err != nil {
			return imageSequence{}, err
		}
		var previous *image.NRGBA
		if frame.dispose == 2 {
			previous = cloneCanvas(canvas)
		}
		op := draw.Src
		if frame.blend == 1 {
			op = draw.Over
		}
		draw.Draw(canvas, frame.bounds, img, img.Bounds().Min, op)
		sequence.frames = append(sequence.frames, cloneCanvas(canvas))
		sequence.delays = append(sequence.delays, frame.delay)
		switch frame.dispose {
		case 1:
			draw.Draw(canvas, frame.bounds, image.Transparent, image.Point{}, draw.Src)
		case 2:
			canvas = previous
		}
	}
	return sequence, nil
}

func decodePNGFrame(ctx context.Context, container pngContainer, frame pngFrame, maximum int64) (image.Image, error) {
	encoded := boundedOutput{maximum: maximum, ctx: ctx}
	if _, err := encoded.Write([]byte(pngSignature)); err != nil {
		return nil, err
	}
	var header [13]byte
	copy(header[:], container.header)
	binary.BigEndian.PutUint32(header[:4], uint32(frame.bounds.Dx()))
	binary.BigEndian.PutUint32(header[4:8], uint32(frame.bounds.Dy()))
	if err := writePNGChunk(&encoded, "IHDR", header[:]); err != nil {
		return nil, err
	}
	if container.palette != nil {
		if err := writePNGChunk(&encoded, "PLTE", container.palette); err != nil {
			return nil, err
		}
	}
	if container.transparency != nil {
		if err := writePNGChunk(&encoded, "tRNS", container.transparency); err != nil {
			return nil, err
		}
	}
	for _, data := range frame.data {
		if err := writePNGChunk(&encoded, "IDAT", data); err != nil {
			return nil, err
		}
	}
	if err := writePNGChunk(&encoded, "IEND"); err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(encoded.data))
	if err != nil {
		return nil, invalid("PNG animation frame decoding failed")
	}
	return img, nil
}

// Force every frame to use RGBA chunks even when a frame happens to be opaque;
// APNG shares one IHDR/color model across every frame.
type rgbaPNGFrame struct{ image.Image }

func (rgbaPNGFrame) Opaque() bool { return false }

func encodeAPNG(ctx context.Context, out *boundedOutput, sequence imageSequence, options encodingOptions, l Limits) error {
	if _, err := out.Write([]byte(pngSignature)); err != nil {
		return err
	}
	var counter uint32
	for i, img := range sequence.frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame := boundedOutput{maximum: l.OutputBytes, ctx: ctx}
		if err := options.encodePNG(&frame, rgbaPNGFrame{img}); err != nil {
			return err
		}
		delay := sequence.delays[i]
		if delay.numerator > 65535 || delay.denominator > 65535 {
			delay = delay.reduced()
		}
		if delay.numerator > 65535 || delay.denominator == 0 || delay.denominator > 65535 {
			return invalid("PNG frame delay cannot represent this animation")
		}
		err := walkPNG(frame.data, func(kind string, body []byte) error {
			switch kind {
			case "IHDR":
				if i == 0 {
					if err := writePNGChunk(out, "IHDR", body); err != nil {
						return err
					}
					var animation [8]byte
					binary.BigEndian.PutUint32(animation[:4], uint32(len(sequence.frames)))
					binary.BigEndian.PutUint32(animation[4:], sequence.plays)
					if err := writePNGChunk(out, "acTL", animation[:]); err != nil {
						return err
					}
				}
				var control [26]byte
				binary.BigEndian.PutUint32(control[:4], counter)
				counter++
				binary.BigEndian.PutUint32(control[4:8], uint32(img.Bounds().Dx()))
				binary.BigEndian.PutUint32(control[8:12], uint32(img.Bounds().Dy()))
				binary.BigEndian.PutUint16(control[20:22], uint16(delay.numerator))
				binary.BigEndian.PutUint16(control[22:24], uint16(delay.denominator))
				// Full canvases replace prior pixels, including transparency.
				return writePNGChunk(out, "fcTL", control[:])
			case "IDAT":
				if i == 0 {
					return writePNGChunk(out, "IDAT", body)
				}
				var prefix [4]byte
				binary.BigEndian.PutUint32(prefix[:], counter)
				counter++
				return writePNGChunk(out, "fdAT", prefix[:], body)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return writePNGChunk(out, "IEND")
}
