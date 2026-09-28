package http

import (
	"math"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// FileResponseLimits bounds a file representation and native range parsing.
// Unseekable streams use Bytes; only seekable downloads parse ranges.
// These limits are independent of buffered JSON response limits. Buffering does
// not grow with Bytes: the response streams through the native fixed copy buffer.
type FileResponseLimits struct {
	Bytes      int64
	Ranges     int
	RangeBytes int
}

func DefaultFileResponseLimits() FileResponseLimits {
	return FileResponseLimits{Bytes: 1 << 30, Ranges: 16, RangeBytes: 4096}
}
func (l FileResponseLimits) Validate() error {
	if l.Bytes <= 0 || l.Ranges <= 0 || l.Ranges > 128 || l.RangeBytes <= 0 || l.RangeBytes > 65536 {
		return fault.New(fault.Invalid, "invalid file response limits")
	}
	// Keep the native sum of ranges and multipart framing representable even
	// when an application configures extraordinarily large virtual sources.
	if l.Bytes > math.MaxInt64/int64(l.Ranges+1) {
		return fault.New(fault.Invalid, "file response limits exceed safe range arithmetic")
	}
	return nil
}

// Count before the native parser can allocate per-range structures. Syntax and
// satisfiability remain native HTTP semantics. Duplicated range headers are
// rejected instead of silently choosing one of several competing declarations.
func (l FileResponseLimits) checkRange(values []string) error {
	if len(values) > 1 {
		return BadRequest
	}
	if len(values) == 0 || values[0] == "" {
		return nil
	}
	if len(values[0]) > l.RangeBytes || strings.Count(values[0], ",") >= l.Ranges {
		return BadRequest
	}
	return nil
}
