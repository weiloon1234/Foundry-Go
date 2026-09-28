package http

import (
	"net/url"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
)

// Disposition selects how a browser should handle a file representation.
// Filename metadata is advisory; it never selects a server filesystem path.
type Disposition string

const (
	DispositionAttachment Disposition = "attachment"
	DispositionInline     Disposition = "inline"
)

func (d Disposition) Validate() error {
	switch d {
	case DispositionAttachment, DispositionInline:
		return nil
	default:
		return fault.New(fault.Invalid, "invalid file content disposition")
	}
}

// HeaderValue supplies a bounded Content-Disposition value with an ASCII
// fallback followed by an RFC6266/RFC5987 UTF-8 filename. It shares display-name
// normalization with uploads and prevents paths or control bytes entering the
// header. The zero disposition is invalid; choose attachment or inline.
func (d Disposition) HeaderValue(name string) (HeaderValue, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	name = filename.Normalize(name, "download")
	fallback := strings.Map(func(r rune) rune {
		// Avoid ambiguous quoted-string escapes and legacy percent decoding.
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == ';' || r == '%' {
			return '_'
		}
		return r
	}, name)
	fallback = strings.TrimSpace(filename.Truncate(fallback, 180))
	if fallback == "" || fallback == "." || fallback == ".." {
		fallback = "download"
	}
	// QueryEscape's alphabet is a valid subset of RFC5987 attr-char. Spaces
	// require percent encoding here; a literal plus is already escaped as %2B.
	encoded := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
	return HeaderValue(string(d) + "; filename=\"" + fallback + "\"; filename*=UTF-8''" + encoded), nil
}
