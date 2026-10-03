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
	HEIF Format = "heif"
	// HEIC is HEIF encoded with HEVC; both names use the canonical HEIF format.
	HEIC     Format = HEIF
	JPEG2000 Format = "jp2"
	JPEGXL   Format = "jxl"
	SVG      Format = "svg"
)

func (f Format) Validate() error {
	if _, ok := portableFormat(f); ok || f.native() {
		return nil
	}
	return invalid("unknown image format")
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
	case HEIF:
		return "image/heif"
	case JPEG2000:
		return "image/jp2"
	case JPEGXL:
		return "image/jxl"
	case SVG:
		return "image/svg+xml"
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
	case "heic":
		value = string(HEIF)
	case "j2k", "j2c", "jpc":
		value = string(JPEG2000)
	case "apng":
		value = "png"
	}
	f := Format(value)
	return f, f.Validate()
}

// CanDecode reports whether the portable engine can read this format.
func (f Format) CanDecode() bool {
	capability, ok := portableFormat(f)
	return ok && capability.Read
}

func invalid(message string) error        { return fault.New(fault.Invalid, message) }
func limited() error                      { return fault.New(fault.Invalid, "image exceeds configured resource limits") }
func unsupported() error                  { return fault.New(fault.Invalid, "unsupported image encoding or container") }
func safeFormat(s fmt.State, text string) { _, _ = s.Write([]byte(text)) }

// ParseMediaType recognizes image media types. Recognition does not imply that
// a particular engine can read or write the format; consult its Capabilities.
func ParseMediaType(media string) (Format, error) {
	media = strings.ToLower(strings.TrimSpace(media))
	switch media {
	case "image/heic":
		return HEIF, nil
	case "image/j2k", "image/jpx":
		return JPEG2000, nil
	}
	for _, capability := range portableFormats() {
		if capability.Format.MediaType() == media {
			return capability.Format, nil
		}
	}
	for _, format := range nativeFormats() {
		if format.MediaType() == media {
			return format, nil
		}
	}
	return "", unsupported()
}
