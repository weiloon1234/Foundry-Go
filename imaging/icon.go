package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"

	"golang.org/x/image/bmp"
)

type iconEntry struct {
	width, height int
	orientation   uint8
	data          []byte
}

func selectIcon(data []byte, l Limits) (iconEntry, int, error) {
	if len(data) < 6 {
		return iconEntry{}, 0, unsupported()
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	if count < 1 || count > l.Frames || count*16 > len(data)-6 {
		return iconEntry{}, 0, limited()
	}
	var selected iconEntry
	var pixels int64
	for i := range count {
		e := data[6+i*16 : 6+(i+1)*16]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if err := l.dimensions(w, h); err != nil {
			return iconEntry{}, 0, err
		}
		pixels += int64(w) * int64(h)
		if pixels > l.Pixels {
			return iconEntry{}, 0, limited()
		}
		n, offset := uint64(binary.LittleEndian.Uint32(e[8:])), uint64(binary.LittleEndian.Uint32(e[12:]))
		if n == 0 || offset < uint64(6+count*16) || offset > uint64(len(data)) || n > uint64(len(data))-offset {
			return iconEntry{}, 0, unsupported()
		}
		entry := iconEntry{width: w, height: h, orientation: 1, data: data[int(offset):int(offset+n)]}
		if bytes.HasPrefix(entry.data, []byte("\x89PNG\r\n\x1a\n")) {
			cfg, err := png.DecodeConfig(bytes.NewReader(entry.data))
			if err != nil || cfg.Width != w || cfg.Height != h {
				return iconEntry{}, 0, unsupported()
			}
			container, err := parsePNG(entry.data, l)
			if err != nil || container.animated {
				return iconEntry{}, 0, unsupported()
			}
			entry.orientation = container.orientation
		} else if _, err := iconBitmap(entry, false); err != nil {
			return iconEntry{}, 0, err
		}
		if w*h > selected.width*selected.height {
			selected = entry
		}
	}
	return selected, count, nil
}

// iconBitmap validates the DIB before adapting it to the existing BMP decoder.
// It supports uncompressed Windows bitmaps and the separate 1-bit AND mask.
func iconBitmap(e iconEntry, decode bool) (image.Image, error) {
	d := e.data
	if len(d) < 40 {
		return nil, unsupported()
	}
	header := int(binary.LittleEndian.Uint32(d))
	if header != 40 && header != 108 && header != 124 || header > len(d) {
		return nil, unsupported()
	}
	w, h := int32(binary.LittleEndian.Uint32(d[4:])), int32(binary.LittleEndian.Uint32(d[8:]))
	bits := int(binary.LittleEndian.Uint16(d[14:]))
	compression := binary.LittleEndian.Uint32(d[16:])
	if w != int32(e.width) || h != int32(e.height*2) || compression != 0 || binary.LittleEndian.Uint16(d[12:]) != 1 {
		return nil, unsupported()
	}
	switch bits {
	case 1, 4, 8, 24, 32:
	default:
		return nil, unsupported()
	}
	colors := int(binary.LittleEndian.Uint32(d[32:]))
	if bits <= 8 {
		if colors == 0 {
			colors = 1 << bits
		}
		if colors > 1<<bits {
			return nil, unsupported()
		}
	} else if colors != 0 {
		return nil, unsupported()
	}
	offset := header + colors*4
	stride := ((e.width*bits + 31) / 32) * 4
	maskStride := ((e.width + 31) / 32) * 4
	size := stride * e.height
	maskSize := maskStride * e.height
	if offset > len(d) || size > len(d)-offset || maskSize > len(d)-offset-size {
		return nil, unsupported()
	}
	if !decode {
		return nil, nil
	}
	bitmap := make([]byte, 14+offset+size)
	copy(bitmap, []byte("BM"))
	binary.LittleEndian.PutUint32(bitmap[2:], uint32(len(bitmap)))
	binary.LittleEndian.PutUint32(bitmap[10:], uint32(14+offset))
	copy(bitmap[14:], d[:offset+size])
	binary.LittleEndian.PutUint32(bitmap[22:], uint32(e.height))
	img, err := bmp.Decode(bytes.NewReader(bitmap))
	if err != nil {
		return nil, err
	}
	mask := d[offset+size : offset+size+maskSize]
	out := image.NewNRGBA(image.Rect(0, 0, e.width, e.height))
	hasAlpha := false
	if bits == 32 {
		for y := range e.height {
			for x := range e.width {
				if d[offset+y*stride+x*4+3] != 0 {
					hasAlpha = true
				}
			}
		}
	}
	for y := range e.height {
		for x := range e.width {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if bits == 32 && hasAlpha {
				c.A = d[offset+(e.height-1-y)*stride+x*4+3]
			} else {
				c.A = 255
			}
			if mask[(e.height-1-y)*maskStride+x/8]&(0x80>>uint(x%8)) != 0 {
				c.A = 0
			}
			out.SetNRGBA(x, y, c)
		}
	}
	return out, nil
}
func decodeIcon(data []byte, l Limits) (image.Image, error) {
	e, _, err := selectIcon(data, l)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(e.data, []byte("\x89PNG\r\n\x1a\n")) {
		return png.Decode(bytes.NewReader(e.data))
	}
	return iconBitmap(e, true)
}
func encodeIcon(w io.Writer, img image.Image, maximum int64) error {
	b := img.Bounds()
	if b.Dx() > 256 || b.Dy() > 256 {
		return invalid("ICO dimensions must not exceed 256")
	}
	pngBytes := boundedOutput{maximum: maximum}
	if err := png.Encode(&pngBytes, img); err != nil {
		return err
	}
	header := make([]byte, 22)
	binary.LittleEndian.PutUint16(header[2:], 1)
	binary.LittleEndian.PutUint16(header[4:], 1)
	header[6], header[7] = byte(b.Dx()%256), byte(b.Dy()%256)
	binary.LittleEndian.PutUint16(header[10:], 1)
	binary.LittleEndian.PutUint16(header[12:], 32)
	binary.LittleEndian.PutUint32(header[14:], uint32(len(pngBytes.data)))
	binary.LittleEndian.PutUint32(header[18:], 22)
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(pngBytes.data)
	return err
}
