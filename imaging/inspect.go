package imaging

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Info is inspected container information, not proof that compressed pixels are
// valid. Process performs full decoding of the selected image. Images counts
// frames, TIFF pages or ICO representations; only animation sets Animated.
type Info struct {
	Format                Format
	Width, Height, Images int
	Animated              bool
	Orientation           uint8
}

// Inspect does no pixel decode and never consults filename or supplied MIME.
// Input must remain unchanged for the duration of this synchronous call.
func Inspect(data []byte, limits Limits) (Info, error) {
	if err := limits.Validate(); err != nil {
		return Info{}, err
	}
	var result Info
	// Framework-owned parsers: contain a panic on hostile input without the
	// goroutine an application-callback boundary would need.
	err := callback.Invoke("inspect image", func() error {
		var err error
		result, err = inspect(data, limits)
		return err
	})
	if err != nil {
		return Info{}, err
	}
	return result, nil
}
func inspect(data []byte, limits Limits) (Info, error) {
	if len(data) == 0 || int64(len(data)) > limits.InputBytes {
		return Info{}, limited()
	}
	if err := limits.admit(int64(len(data)), 0); err != nil {
		return Info{}, err
	}
	info := Info{Images: 1, Orientation: 1}
	var cfg image.Config
	var err error
	r := bytes.NewReader(data)
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		info.Format = JPEG
		cfg, err = jpeg.DecodeConfig(r)
		if err == nil {
			info.Orientation, err = jpegOrientation(data)
		}
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		info.Format = PNG
		cfg, err = png.DecodeConfig(r)
		if err == nil {
			info.Images, info.Orientation, err = pngDetails(data)
			info.Animated = info.Images > 1
		}
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		info.Format = WebP
		info.Images, info.Orientation, err = webpDetails(data)
		info.Animated = info.Images > 1
		// x/image has no animated WebP decoder; do not silently take a frame.
		if err == nil && info.Animated {
			err = unsupported()
		}
		if err == nil {
			cfg, err = webp.DecodeConfig(r)
		}
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		info.Format = GIF
		cfg, err = gif.DecodeConfig(r)
		if err == nil {
			info.Images, err = gifImages(data, limits)
			info.Animated = info.Images > 1
		}
	case bytes.HasPrefix(data, []byte("BM")):
		info.Format = BMP
		cfg, err = bmp.DecodeConfig(r)
	case bytes.HasPrefix(data, []byte("II\x2a\x00")) || bytes.HasPrefix(data, []byte("MM\x00\x2a")):
		info.Format = TIFF
		info.Images, info.Orientation, err = tiffDetails(data, limits)
		if err == nil {
			cfg, err = tiff.DecodeConfig(r)
		}
	case bytes.HasPrefix(data, []byte{0, 0, 1, 0}):
		info.Format = ICO
		var entry iconEntry
		entry, info.Images, err = selectIcon(data, limits)
		cfg.Width, cfg.Height = entry.width, entry.height
		info.Orientation = entry.orientation
	default:
		return Info{}, unsupported()
	}
	if err != nil {
		return Info{}, invalid("invalid or unsupported image container")
	}
	info.Width, info.Height = cfg.Width, cfg.Height
	if err := limits.dimensions(info.Width, info.Height); err != nil {
		return Info{}, err
	}
	if info.Images < 1 || info.Images > limits.Frames {
		return Info{}, limited()
	}
	if info.Animated && int64(info.Images)*int64(info.Width)*int64(info.Height) > limits.Pixels {
		return Info{}, limited()
	}
	// Decoding is the first phase: the input stays retained beside the
	// decoder's buffers for this format.
	if err := limits.admit(int64(len(data)), int64(info.Width)*int64(info.Height)*info.Format.decodePeakBytes()); err != nil {
		return Info{}, err
	}
	return info, nil
}

func gifImages(data []byte, l Limits) (int, error) {
	if len(data) < 13 {
		return 0, unsupported()
	}
	pos := 13
	if data[10]&128 != 0 {
		pos += 3 * (1 << ((data[10] & 7) + 1))
	}
	count := 0
	var pixels int64
	skipBlocks := func() bool {
		for pos < len(data) {
			n := int(data[pos])
			pos++
			if n == 0 {
				return true
			}
			if n > len(data)-pos {
				return false
			}
			pos += n
		}
		return false
	}
	for pos < len(data) {
		kind := data[pos]
		pos++
		switch kind {
		case 0x3b:
			if count == 0 || pos != len(data) {
				return 0, unsupported()
			}
			return count, nil
		case 0x21:
			if pos >= len(data) {
				return 0, unsupported()
			}
			pos++
			if !skipBlocks() {
				return 0, unsupported()
			}
		case 0x2c:
			if len(data)-pos < 9 {
				return 0, unsupported()
			}
			w, h := int(binary.LittleEndian.Uint16(data[pos+4:])), int(binary.LittleEndian.Uint16(data[pos+6:]))
			if err := l.dimensions(w, h); err != nil {
				return 0, err
			}
			pixels += int64(w) * int64(h)
			count++
			if count > l.Frames || pixels > l.Pixels {
				return 0, limited()
			}
			flags := data[pos+8]
			pos += 9
			if flags&128 != 0 {
				pos += 3 * (1 << ((flags & 7) + 1))
			}
			if pos >= len(data) {
				return 0, unsupported()
			}
			pos++
			if !skipBlocks() {
				return 0, unsupported()
			}
		default:
			return 0, unsupported()
		}
	}
	return 0, unsupported()
}

func pngDetails(data []byte) (int, uint8, error) {
	frames, orientation := 1, uint8(1)
	seenAnimation, seenExif := false, false
	for p := 8; p < len(data); {
		if len(data)-p < 12 {
			return 0, 0, unsupported()
		}
		n := uint64(binary.BigEndian.Uint32(data[p:]))
		if n > uint64(len(data)-p-12) {
			return 0, 0, unsupported()
		}
		kind := string(data[p+4 : p+8])
		body := data[p+8 : p+8+int(n)]
		if crc32.ChecksumIEEE(data[p+4:p+8+int(n)]) != binary.BigEndian.Uint32(data[p+8+int(n):p+12+int(n)]) {
			return 0, 0, unsupported()
		}
		switch kind {
		case "acTL":
			if seenAnimation {
				return 0, 0, unsupported()
			}
			seenAnimation = true
			if len(body) != 8 {
				return 0, 0, unsupported()
			}
			frames = int(binary.BigEndian.Uint32(body))
			if frames < 1 {
				return 0, 0, unsupported()
			}
		case "eXIf":
			if seenExif {
				return 0, 0, unsupported()
			}
			seenExif = true
			var err error
			orientation, err = exifOrientation(body)
			if err != nil {
				return 0, 0, err
			}
		}
		p += 12 + int(n)
		if kind == "IEND" {
			if n != 0 || p != len(data) {
				return 0, 0, unsupported()
			}
			return frames, orientation, nil
		}
	}
	return 0, 0, unsupported()
}

func webpDetails(data []byte) (int, uint8, error) {
	if len(data) < 12 || uint64(binary.LittleEndian.Uint32(data[4:]))+8 != uint64(len(data)) {
		return 0, 0, unsupported()
	}
	frames, orientation := 1, uint8(1)
	for p := 12; p < len(data); {
		if len(data)-p < 8 {
			return 0, 0, unsupported()
		}
		n := uint64(binary.LittleEndian.Uint32(data[p+4:]))
		padded := n + (n & 1)
		if padded > uint64(len(data)-p-8) {
			return 0, 0, unsupported()
		}
		kind := string(data[p : p+4])
		body := data[p+8 : p+8+int(n)]
		switch kind {
		case "VP8X":
			if len(body) != 10 {
				return 0, 0, unsupported()
			}
			if body[0]&2 != 0 {
				frames = 2
			}
		case "ANIM", "ANMF":
			frames = 2
		case "EXIF":
			var err error
			orientation, err = exifOrientation(body)
			if err != nil {
				return 0, 0, err
			}
		}
		p += 8 + int(padded)
	}
	return frames, orientation, nil
}
