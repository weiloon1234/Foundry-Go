package imaging

import (
	"image"
	"time"
)

// Limits apply before pixel decoding, before each transform, and to output.
// WorkingBytes bounds the admitted workspace of each pipeline phase: the
// retained encoded input plus the pixel buffers live in that phase (decoder
// buffers, each transform's input, output and scratch, and the encoder's
// working copies with the owned output allowance). Per-pixel costs come from
// measurements of the engine's codecs (see the imaging guide). It is not a
// process heap or OS memory quota; codec/runtime bookkeeping and garbage
// collection are separate. There is deliberately no unbounded input option.
type Limits struct {
	InputBytes, OutputBytes int64
	Width, Height           int
	Pixels                  int64
	Frames                  int
	WorkingBytes            int64
}

func DefaultLimits() Limits {
	return Limits{InputBytes: 50 << 20, OutputBytes: 50 << 20, Width: 12000, Height: 12000, Pixels: 25000000, Frames: 64, WorkingBytes: 640 << 20}
}
func (l Limits) Validate() error {
	if l.InputBytes < 1 || l.InputBytes > 1<<30 || l.OutputBytes < 1 || l.OutputBytes > 1<<30 || l.Width < 1 || l.Width > 65535 || l.Height < 1 || l.Height > 65535 || l.Pixels < 1 || l.Pixels > 1<<30 || l.Frames < 1 || l.Frames > 1024 || l.WorkingBytes < 1 || l.WorkingBytes > 16<<30 {
		return invalid("invalid image limits")
	}
	return nil
}
func (l Limits) dimensions(w, h int) error {
	if w < 1 || h < 1 || w > l.Width || h > l.Height || int64(w)*int64(h) > l.Pixels {
		return limited()
	}
	return nil
}

// admit checks one pipeline phase: the retained encoded input plus that
// phase's live buffers. Arithmetic is bounded by Validate.
func (l Limits) admit(input, phase int64) error {
	if input+phase > l.WorkingBytes {
		return limited()
	}
	return nil
}

// Working bytes per pixel. Transforms always produce 8-bit NRGBA. Resampling
// and blur allocate intermediates measured at under 8 bytes per pixel of their
// largest canvas; geometric and color transforms need no scratch.
const (
	transformBytes = 4
	scratchBytes   = 8
)

// decodePeakBytes bounds decoder memory per source pixel while decoding.
// Progressive JPEG retains coefficient blocks (4 bytes per sample) beside the
// image; 16-bit PNG/TIFF decode to 8 bytes per pixel; lossless WebP keeps an
// ARGB plane and transform buffers beside its NRGBA result.
func (f Format) decodePeakBytes() int64 {
	switch f {
	case JPEG:
		return 20
	case PNG, TIFF:
		return 9
	case WebP:
		return 12
	case GIF:
		return 6
	default:
		return 8
	}
}

// decodedBytes bounds the decoded image retained after decoding.
func (f Format) decodedBytes() int64 {
	switch f {
	case PNG, TIFF, AVIF:
		return 8
	default:
		return 4
	}
}

// encodeWorkspace bounds an encoder's working copies, excluding
// the owned output allowance. WebP includes color/alpha planes, references,
// entropy searches, internal compressed buffers and fixed histograms. Lossy
// macroblock storage is rounded up even for very narrow images.
func (p Plan) encodeWorkspace(f Format, bounds image.Rectangle) int64 {
	pixels := int64(bounds.Dx()) * int64(bounds.Dy())
	if p.nativeOutput(f) {
		return pixels*128 + 16<<20
	}
	switch f {
	case WebP:
		if p.encoding.webpLossy() {
			padded := int64((bounds.Dx()+15)/16*16) * int64((bounds.Dy()+15)/16*16)
			return padded*192 + 16<<20
		}
		return pixels*128 + 16<<20
	case AVIF:
		return pixels * 6
	case ICO:
		return pixels * 8
	default:
		return pixels * 2
	}
}

type Config struct {
	Backend   Backend
	Limits    Limits
	MaxActive int
	Timeout   time.Duration
}

func DefaultConfig() Config {
	return Config{Backend: AutoBackend, Limits: DefaultLimits(), MaxActive: 2, Timeout: time.Minute}
}
func (c Config) Validate() error {
	if err := c.Limits.Validate(); err != nil {
		return err
	}
	if c.Backend > AutoBackend || c.MaxActive < 1 || c.MaxActive > 64 || c.Timeout <= 0 || c.Timeout > 10*time.Minute {
		return invalid("invalid image engine configuration")
	}
	return nil
}
