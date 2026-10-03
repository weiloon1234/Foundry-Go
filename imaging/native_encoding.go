package imaging

import "github.com/weiloon1234/Foundry-Go/value"

type nativeEncoding struct {
	heif, jpeg2000, jpegxl value.Optional[int]
}

// HEIFQuality selects HEVC quality 1..100, default 85. Requires explicit HEIF
// output and LibvipsBackend. A quality of 100 is not a lossless guarantee.
func (p Plan) HEIFQuality(quality int) Plan { p.nativeEncoding.heif = value.Set(quality); return p }

// JPEG2000Quality selects lossy quality 1..100 for explicit JPEG2000 output.
// Omitting it keeps the default lossless encoding. Requires LibvipsBackend.
func (p Plan) JPEG2000Quality(quality int) Plan {
	p.nativeEncoding.jpeg2000 = value.Set(quality)
	return p
}

// JPEGXLQuality selects lossy quality 1..100 for explicit JPEGXL output.
// Omitting it keeps the default lossless encoding. Requires LibvipsBackend.
func (p Plan) JPEGXLQuality(quality int) Plan { p.nativeEncoding.jpegxl = value.Set(quality); return p }

func (o nativeEncoding) validate(format Format) error {
	for _, option := range []struct {
		format  Format
		quality value.Optional[int]
	}{{HEIF, o.heif}, {JPEG2000, o.jpeg2000}, {JPEGXL, o.jpegxl}} {
		if q, set := option.quality.Get(); set && (format != option.format || q < 1 || q > 100) {
			return invalid("native image quality requires its explicit format and a value from 1 to 100")
		}
	}
	return nil
}
