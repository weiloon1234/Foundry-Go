package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"

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

// inspection retains codec admission details without adding mutable/private fields
// to the public value returned to consumers.
type inspection struct {
	Info
	avif   *avifContainer
	webp   *webpContainer
	native bool
}

func (i inspection) decodeWorkspace() int64 {
	if i.native {
		return int64(i.Width)*int64(i.Height)*64 + 16<<20
	}
	if i.webp != nil {
		return i.webp.workspace()
	}
	if i.avif != nil {
		return i.avif.workspace()
	}
	return int64(i.Width) * int64(i.Height) * i.Format.decodePeakBytes()
}

// Inspect does no pixel decode and never consults filename or supplied MIME.
// Input must remain unchanged for the duration of this synchronous call.
func Inspect(data []byte, limits Limits) (Info, error) {
	if err := limits.Validate(); err != nil {
		return Info{}, err
	}
	var result inspection
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
	return result.Info, nil
}
func inspect(data []byte, limits Limits) (inspection, error) {
	if len(data) == 0 || int64(len(data)) > limits.InputBytes {
		return inspection{}, limited()
	}
	if err := limits.admit(int64(len(data)), 0); err != nil {
		return inspection{}, err
	}
	info := inspection{Info: Info{Images: 1, Orientation: 1}}
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
			var container pngContainer
			container, err = parsePNG(data, limits)
			info.Orientation, info.Animated = container.orientation, container.animated
			if container.animated {
				info.Images = len(container.frames)
			}
		}
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		info.Format = WebP
		info.webp, err = parseWebP(data, limits)
		if err == nil {
			cfg.Width, cfg.Height = info.webp.width, info.webp.height
			info.Images, info.Animated, info.Orientation = len(info.webp.frames), info.webp.animated, info.webp.orientation
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
	case len(data) >= 8 && string(data[4:8]) == "ftyp":
		info.Format = AVIF
		info.avif, err = inspectAVIF(data, limits)
		if err == nil {
			primary := info.avif.items[info.avif.primary]
			cfg.Width, cfg.Height = primary.width, primary.height
			info.Orientation = info.avif.orientation
			if track := info.avif.track; track != nil {
				cfg.Width, cfg.Height = track.width, track.height
				info.Images, info.Animated = len(track.samples), true
			}
		}
	case bytes.HasPrefix(data, []byte{0, 0, 1, 0}):
		info.Format = ICO
		var entry iconEntry
		entry, info.Images, err = selectIcon(data, limits)
		cfg.Width, cfg.Height = entry.width, entry.height
		info.Orientation = entry.orientation
	default:
		return inspection{}, unsupported()
	}
	if err != nil {
		return inspection{}, invalid("invalid or unsupported image container")
	}
	info.Width, info.Height = cfg.Width, cfg.Height
	if err := limits.dimensions(info.Width, info.Height); err != nil {
		return inspection{}, err
	}
	if info.Images < 1 || info.Images > limits.Frames {
		return inspection{}, limited()
	}
	if info.Animated && int64(info.Images)*int64(info.Width)*int64(info.Height) > limits.Pixels {
		return inspection{}, limited()
	}
	// Decoding is the first phase: the input stays retained beside the
	// decoder's buffers for this format.
	if err := limits.admit(int64(len(data)), info.decodeWorkspace()); err != nil {
		return inspection{}, err
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
