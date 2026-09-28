// Package imaging inspects and transforms bounded, untrusted raster images.
// Plans are immutable declarations. An Engine owns concurrent processing calls;
// callers retain ownership of input readers and the engine's lifecycle.
package imaging

import (
	"fmt"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type Format string

const (
	JPEG Format = "jpeg"
	PNG  Format = "png"
	WebP Format = "webp"
	GIF  Format = "gif"
	BMP  Format = "bmp"
	TIFF Format = "tiff"
	AVIF Format = "avif"
	ICO  Format = "ico"
)

func (f Format) Validate() error {
	switch f {
	case JPEG, PNG, WebP, GIF, BMP, TIFF, AVIF, ICO:
		return nil
	default:
		return invalid("unknown image format")
	}
}
func (f Format) Extension() string {
	if f == JPEG {
		return "jpg"
	}
	return string(f)
}
func (f Format) MediaType() string {
	switch f {
	case JPEG:
		return "image/jpeg"
	case PNG:
		return "image/png"
	case WebP:
		return "image/webp"
	case GIF:
		return "image/gif"
	case BMP:
		return "image/bmp"
	case TIFF:
		return "image/tiff"
	case AVIF:
		return "image/avif"
	case ICO:
		return "image/vnd.microsoft.icon"
	default:
		return ""
	}
}

// ParseExtension is a filename convenience, never media validation.
func ParseExtension(extension string) (Format, error) {
	value := strings.ToLower(strings.TrimPrefix(extension, "."))
	switch value {
	case "jpg":
		value = "jpeg"
	case "tif":
		value = "tiff"
	}
	f := Format(value)
	return f, f.Validate()
}

// CanDecode is explicit: AVIF is an encoding format, matching the reference
// framework's default codec build. AVIF input is rejected before a decoder runs.
func (f Format) CanDecode() bool { return f.Validate() == nil && f != AVIF }

func invalid(message string) error        { return fault.New(fault.Invalid, message) }
func limited() error                      { return fault.New(fault.Invalid, "image exceeds configured resource limits") }
func unsupported() error                  { return fault.New(fault.Invalid, "unsupported image encoding or container") }
func safeFormat(s fmt.State, text string) { _, _ = s.Write([]byte(text)) }
