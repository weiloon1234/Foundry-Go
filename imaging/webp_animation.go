package imaging

import (
	"context"
	"encoding/binary"
	"io"
)

func encodeWebPSequence(ctx context.Context, out *boundedOutput, sequence imageSequence, plan Plan, l Limits) error {
	if sequence.plays > 65535 {
		return invalid("WebP loop count cannot represent this animation")
	}
	if _, err := out.Write([]byte("RIFF\x00\x00\x00\x00WEBP")); err != nil {
		return err
	}
	bounds := sequence.frames[0].Bounds()
	var extended [10]byte
	extended[0] = 2 // Animation; alpha is added if any encoded frame uses it.
	putWebPUint24(extended[4:7], uint32(bounds.Dx()-1))
	putWebPUint24(extended[7:10], uint32(bounds.Dy()-1))
	if err := writeWebPChunk(out, "VP8X", extended[:]); err != nil {
		return err
	}
	var animation [6]byte // Full replacement canvases use transparent background.
	binary.LittleEndian.PutUint16(animation[4:], uint16(sequence.plays))
	if err := writeWebPChunk(out, "ANIM", animation[:]); err != nil {
		return err
	}
	for i, img := range sequence.frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		delay, err := sequence.delays[i].ticks(1000, 0xffffff)
		if err != nil {
			return invalid("WebP frame delay cannot represent this animation")
		}
		frame := boundedOutput{maximum: l.OutputBytes, ctx: ctx}
		if err := plan.encode(&frame, img, WebP, l.OutputBytes); err != nil {
			return err
		}
		if frame.err != nil {
			return frame.err
		}
		// Reuse the container validator when extracting the encoded bitstream.
		encodedLimits := l
		encodedLimits.InputBytes = l.OutputBytes
		container, err := parseWebP(frame.data, encodedLimits)
		if err != nil || container.animated || container.width != bounds.Dx() || container.height != bounds.Dy() {
			return invalid("WebP encoder produced an invalid frame")
		}
		coded := container.frames[0]
		if coded.alpha != nil || coded.kind == "VP8L" && coded.bitstream[4]&0x10 != 0 {
			out.data[20] |= 0x10
		}
		var control [16]byte
		putWebPUint24(control[6:9], uint32(bounds.Dx()-1))
		putWebPUint24(control[9:12], uint32(bounds.Dy()-1))
		putWebPUint24(control[12:15], delay)
		control[15] = 2 // Replace the full canvas, including transparent pixels.
		// Only image subchunks belong in ANMF. A still encoder may also emit
		// VP8X or metadata; those outer chunks must never be nested in a frame.
		size := uint64(16 + 8 + len(coded.bitstream) + len(coded.bitstream)%2)
		if coded.alpha != nil {
			size += uint64(8 + len(coded.alpha) + len(coded.alpha)%2)
		}
		if err := writeWebPChunkHeader(out, "ANMF", size); err != nil {
			return err
		}
		if _, err := out.Write(control[:]); err != nil {
			return err
		}
		if coded.alpha != nil {
			if err := writeWebPChunk(out, "ALPH", coded.alpha); err != nil {
				return err
			}
		}
		if err := writeWebPChunk(out, coded.kind, coded.bitstream); err != nil {
			return err
		}
	}
	binary.LittleEndian.PutUint32(out.data[4:], uint32(len(out.data)-8))
	return nil
}

func writeWebPChunk(w io.Writer, kind string, parts ...[]byte) error {
	var size uint64
	for _, part := range parts {
		size += uint64(len(part))
	}
	if err := writeWebPChunkHeader(w, kind, size); err != nil {
		return err
	}
	for _, part := range parts {
		if _, err := w.Write(part); err != nil {
			return err
		}
	}
	if size&1 != 0 {
		_, err := w.Write([]byte{0})
		return err
	}
	return nil
}

func writeWebPChunkHeader(w io.Writer, kind string, size uint64) error {
	if len(kind) != 4 || size > 1<<32-1 {
		return limited()
	}
	var header [8]byte
	copy(header[:4], kind)
	binary.LittleEndian.PutUint32(header[4:], uint32(size))
	_, err := w.Write(header[:])
	return err
}
