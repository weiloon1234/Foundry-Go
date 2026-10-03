package imaging

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"io"
)

const pngSignature = "\x89PNG\r\n\x1a\n"
const maxPNGChunks = 65536

type pngFrame struct {
	bounds         image.Rectangle
	delay          frameDelay
	dispose, blend byte
	data           [][]byte
}

type pngContainer struct {
	header                []byte
	palette, transparency []byte
	frames                []pngFrame
	plays                 uint32
	orientation           uint8
	animated              bool
}

// walkPNG owns chunk bounds and CRC validation for inspection, frame decoding
// and animation encoding. Callback data aliases the caller's input.
func walkPNG(data []byte, visit func(string, []byte) error) error {
	if !bytes.HasPrefix(data, []byte(pngSignature)) {
		return unsupported()
	}
	count := 0
	for pos := len(pngSignature); pos < len(data); {
		count++
		if count > maxPNGChunks {
			return limited()
		}
		if len(data)-pos < 12 {
			return unsupported()
		}
		n := uint64(binary.BigEndian.Uint32(data[pos:]))
		if n > uint64(len(data)-pos-12) {
			return unsupported()
		}
		end := pos + 8 + int(n)
		if crc32.ChecksumIEEE(data[pos+4:end]) != binary.BigEndian.Uint32(data[end:]) {
			return unsupported()
		}
		kind := string(data[pos+4 : pos+8])
		for _, c := range []byte(kind) {
			if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return unsupported()
			}
		}
		if kind[2]&0x20 != 0 {
			return unsupported()
		}
		if err := visit(kind, data[pos+8:end]); err != nil {
			return err
		}
		pos = end + 4
		if kind == "IEND" {
			if n != 0 || pos != len(data) {
				return unsupported()
			}
			return nil
		}
	}
	return unsupported()
}

func parsePNG(data []byte, l Limits) (pngContainer, error) {
	p := pngContainer{orientation: 1}
	expected := 0
	var width, height int
	var sequence uint64
	seenIDAT, endedIDAT, seenExif, defaultFrame := false, false, false, false
	nextSequence := func(body []byte) error {
		if len(body) < 4 || uint64(binary.BigEndian.Uint32(body)) != sequence {
			return unsupported()
		}
		sequence++
		return nil
	}
	err := walkPNG(data, func(kind string, body []byte) error {
		if p.header == nil && kind != "IHDR" {
			return unsupported()
		}
		if seenIDAT && kind != "IDAT" {
			endedIDAT = true
		}
		switch kind {
		case "IHDR":
			if p.header != nil || len(body) != 13 {
				return unsupported()
			}
			width, height = int(binary.BigEndian.Uint32(body)), int(binary.BigEndian.Uint32(body[4:]))
			if err := l.dimensions(width, height); err != nil {
				return err
			}
			p.header = body
		case "PLTE":
			if seenIDAT || p.palette != nil || len(body) == 0 || len(body) > 768 || len(body)%3 != 0 {
				return unsupported()
			}
			p.palette = body
		case "tRNS":
			if seenIDAT || p.transparency != nil || len(body) == 0 || len(body) > 256 {
				return unsupported()
			}
			p.transparency = body
		case "acTL":
			if p.animated || seenIDAT || len(body) != 8 {
				return unsupported()
			}
			expected = int(binary.BigEndian.Uint32(body))
			if expected < 1 || expected > l.Frames || int64(expected)*int64(width)*int64(height) > l.Pixels {
				return limited()
			}
			p.animated, p.plays = true, binary.BigEndian.Uint32(body[4:])
		case "fcTL":
			if !p.animated || len(body) != 26 || len(p.frames) >= expected {
				return unsupported()
			}
			if err := nextSequence(body); err != nil {
				return err
			}
			if len(p.frames) > 0 && len(p.frames[len(p.frames)-1].data) == 0 {
				return unsupported()
			}
			w, h, x, y := int64(binary.BigEndian.Uint32(body[4:])), int64(binary.BigEndian.Uint32(body[8:])), int64(binary.BigEndian.Uint32(body[12:])), int64(binary.BigEndian.Uint32(body[16:]))
			if w < 1 || h < 1 || x+w > int64(width) || y+h > int64(height) || body[24] > 2 || body[25] > 1 {
				return unsupported()
			}
			if !seenIDAT {
				if len(p.frames) != 0 || x != 0 || y != 0 || w != int64(width) || h != int64(height) {
					return unsupported()
				}
				defaultFrame = true
			}
			den := uint32(binary.BigEndian.Uint16(body[22:]))
			if den == 0 {
				den = 100
			}
			p.frames = append(p.frames, pngFrame{bounds: image.Rect(int(x), int(y), int(x+w), int(y+h)), delay: frameDelay{uint32(binary.BigEndian.Uint16(body[20:])), den}, dispose: body[24], blend: body[25]})
		case "IDAT":
			if endedIDAT {
				return unsupported()
			}
			seenIDAT = true
			if defaultFrame {
				p.frames[0].data = append(p.frames[0].data, body)
			}
		case "fdAT":
			if !p.animated || !seenIDAT || len(body) < 4 || len(p.frames) == 0 || (defaultFrame && len(p.frames) == 1) {
				return unsupported()
			}
			if err := nextSequence(body); err != nil {
				return err
			}
			last := &p.frames[len(p.frames)-1]
			last.data = append(last.data, body[4:])
		case "eXIf":
			if seenExif {
				return unsupported()
			}
			seenExif = true
			var err error
			p.orientation, err = exifOrientation(body)
			return err
		case "IEND":
			if !seenIDAT || (p.animated && (len(p.frames) != expected || len(p.frames[len(p.frames)-1].data) == 0)) {
				return unsupported()
			}
		default:
			// Unknown critical chunks are not safe to discard when a frame
			// is reconstructed. Ancillary metadata is deliberately stripped.
			if kind[0]&0x20 == 0 {
				return unsupported()
			}
		}
		return nil
	})
	return p, err
}

func writePNGChunk(w io.Writer, kind string, parts ...[]byte) error {
	n := 0
	for _, part := range parts {
		n += len(part)
	}
	var header [8]byte
	binary.BigEndian.PutUint32(header[:4], uint32(n))
	copy(header[4:], kind)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	checksum := crc32.Update(0, crc32.IEEETable, header[4:])
	for _, part := range parts {
		if _, err := w.Write(part); err != nil {
			return err
		}
		checksum = crc32.Update(checksum, crc32.IEEETable, part)
	}
	var footer [4]byte
	binary.BigEndian.PutUint32(footer[:], checksum)
	_, err := w.Write(footer[:])
	return err
}
