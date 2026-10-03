package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"

	"golang.org/x/image/vp8"
	"golang.org/x/image/vp8l"
)

// Compressed payloads borrow the input for one synchronous engine operation.
// Inspection checks coded dimensions, not just VP8X/ANMF declarations.
type webpContainer struct {
	width, height int
	animated      bool
	orientation   uint8
	plays         uint32
	background    color.NRGBA
	frames        []webpFrame
}

type webpFrame struct {
	bounds           image.Rectangle
	delay            frameDelay
	dispose, noBlend bool
	kind             string
	bitstream, alpha []byte
}

func (c *webpContainer) workspace() int64 {
	pixels := int64(c.width) * int64(c.height)
	peak := pixels * WebP.decodePeakBytes()
	for _, frame := range c.frames {
		if frame.kind == "VP8 " {
			// VP8 owns complete 16x16 macroblocks, including narrow edge
			// images whose padding cannot be bounded by visible pixels alone.
			padded := int64((frame.bounds.Dx()+15)/16*16) * int64((frame.bounds.Dy()+15)/16*16)
			peak = max(peak, padded*WebP.decodePeakBytes())
		}
	}
	if c.animated {
		peak += pixels * transformBytes // FirstFrame still composes onto a canvas.
	}
	return peak + int64(len(c.frames)+1)*256
}

func parseWebP(data []byte, l Limits) (*webpContainer, error) {
	if len(data) < 12 || int64(len(data)) > l.InputBytes || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:]))+8 != uint64(len(data)) {
		return nil, unsupported()
	}
	c := &webpContainer{orientation: 1}
	var haveCanvas, haveAnimation, haveProfile bool
	var still webpFrame
	chunks := 0
	err := walkWebPChunks(data[12:], &chunks, func(kind string, body []byte) error {
		switch kind {
		case "VP8X":
			if haveCanvas || haveProfile || haveAnimation || still.kind != "" || still.alpha != nil || len(body) != 10 {
				return unsupported()
			}
			haveCanvas = true
			c.width, c.height = 1+int(webpUint24(body[4:7])), 1+int(webpUint24(body[7:10]))
			c.animated = body[0]&2 != 0
			return l.dimensions(c.width, c.height)
		case "ICCP":
			if !haveCanvas || haveProfile || haveAnimation || still.kind != "" || still.alpha != nil {
				return unsupported()
			}
			haveProfile = true // Portable processing deliberately strips profiles.
		case "ANIM":
			if !c.animated {
				return nil // The specification requires ignoring an unset animation flag.
			}
			if haveAnimation || len(body) != 6 {
				return unsupported()
			}
			haveAnimation = true
			c.background = color.NRGBA{R: body[2], G: body[1], B: body[0], A: body[3]}
			c.plays = uint32(binary.LittleEndian.Uint16(body[4:]))
		case "ANMF":
			if !c.animated || !haveAnimation || len(body) < 16 {
				return unsupported()
			}
			if len(c.frames) >= l.Frames || int64(len(c.frames)+1)*int64(c.width)*int64(c.height) > l.Pixels {
				return limited()
			}
			x, y := int(webpUint24(body[:3]))*2, int(webpUint24(body[3:6]))*2
			w, h := int(webpUint24(body[6:9]))+1, int(webpUint24(body[9:12]))+1
			frame := webpFrame{bounds: image.Rect(x, y, x+w, y+h), delay: frameDelay{webpUint24(body[12:15]), 1000}, dispose: body[15]&1 != 0, noBlend: body[15]&2 != 0}
			if !frame.bounds.In(image.Rect(0, 0, c.width, c.height)) {
				return unsupported()
			}
			if err := walkWebPChunks(body[16:], &chunks, frame.chunk); err != nil {
				return err
			}
			if err := frame.validate(l); err != nil {
				return err
			}
			c.frames = append(c.frames, frame)
		case "VP8 ", "VP8L", "ALPH":
			if c.animated || kind == "ALPH" && !haveCanvas {
				return unsupported()
			}
			return still.chunk(kind, body)
		case "EXIF":
			var err error
			c.orientation, err = exifOrientation(body)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if c.animated {
		if !haveAnimation || len(c.frames) == 0 {
			return nil, unsupported()
		}
	} else {
		if haveCanvas {
			still.bounds = image.Rect(0, 0, c.width, c.height)
		}
		if err := still.validate(l); err != nil {
			return nil, err
		}
		c.width, c.height = still.bounds.Dx(), still.bounds.Dy()
		c.frames = []webpFrame{still}
	}
	return c, nil
}

// A shared counter includes nested frame chunks; unknown chunks and reserved
// flag bits are ignored, but truncated payloads and nonzero padding are invalid.
func walkWebPChunks(data []byte, count *int, visit func(string, []byte) error) error {
	for len(data) != 0 {
		(*count)++
		if *count > 65536 {
			return limited()
		}
		if len(data) < 8 {
			return unsupported()
		}
		n := uint64(binary.LittleEndian.Uint32(data[4:]))
		padded := n + (n & 1)
		if padded > uint64(len(data)-8) || n&1 != 0 && data[8+int(n)] != 0 {
			return unsupported()
		}
		if err := visit(string(data[:4]), data[8:8+int(n)]); err != nil {
			return err
		}
		data = data[8+int(padded):]
	}
	return nil
}

func (f *webpFrame) chunk(kind string, body []byte) error {
	switch kind {
	case "ALPH":
		if f.alpha != nil || f.kind != "" || len(body) < 2 || body[0]&3 > 1 {
			return unsupported()
		}
		f.alpha = body
	case "VP8 ", "VP8L":
		if f.kind != "" || kind == "VP8L" && f.alpha != nil {
			return unsupported()
		}
		f.kind, f.bitstream = kind, body
	case "VP8X", "ANIM", "ANMF", "ICCP":
		return unsupported()
	}
	return nil
}

func (f *webpFrame) validate(l Limits) error {
	if f.kind == "" {
		return unsupported()
	}
	coded, err := webpFrameDimensions(f.kind, f.bitstream)
	if err != nil {
		return err
	}
	if err := l.dimensions(coded.X, coded.Y); err != nil {
		return err
	}
	if f.bounds.Empty() {
		f.bounds = image.Rectangle{Max: coded}
	} else if f.bounds.Size() != coded {
		return unsupported()
	}
	if f.alpha != nil && f.alpha[0]&3 == 0 && int64(len(f.alpha)-1) != int64(coded.X)*int64(coded.Y) {
		return unsupported()
	}
	return nil
}

func webpFrameDimensions(kind string, data []byte) (image.Point, error) {
	reader := bytes.NewReader(data)
	if kind == "VP8L" {
		config, err := vp8l.DecodeConfig(reader)
		return image.Pt(config.Width, config.Height), err
	}
	decoder := vp8.NewDecoder()
	decoder.Init(reader, len(data))
	header, err := decoder.DecodeFrameHeader()
	if err == nil && (!header.KeyFrame || !header.ShowFrame) {
		return image.Point{}, unsupported()
	}
	return image.Pt(header.Width, header.Height), err
}

func webpUint24(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
}

func putWebPUint24(data []byte, n uint32) {
	data[0], data[1], data[2] = byte(n), byte(n>>8), byte(n>>16)
}
