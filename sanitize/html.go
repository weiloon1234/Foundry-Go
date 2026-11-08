// Package sanitize supplies immutable HTML fragment allowlists backed by
// bluemonday. Output is for HTML body content, not scripts, CSS, URLs or attributes.
package sanitize

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const MaxInputBytes = 1 << 20
const MaxOutputBytes = 4 << 20

// URLScheme is an additional URL scheme supported by a sanitization policy.
type URLScheme string

// URLSchemeTel permits telephone links in addition to HTTP, HTTPS and mailto.
const URLSchemeTel URLScheme = "tel"

// Config configures allowed HTML tags, URLs and byte budgets.
type Config struct {
	AllowedTags []string
	InputBytes  int
	OutputBytes int

	// AdditionalURLSchemes accepts only URLSchemeTel. Unknown values and
	// duplicates are invalid; the default HTTP, HTTPS and mailto schemes cannot be added.
	AdditionalURLSchemes []URLScheme

	// AllowRelativeURLs permits local paths, queries and fragments. Protocol-relative
	// URLs (//host) and backslash authority prefixes remain forbidden.
	AllowRelativeURLs bool
}

// DefaultConfig returns default tags and byte budgets, with only HTTP, HTTPS and
// mailto URLs allowed and relative URLs disabled.
func DefaultConfig() Config {
	return Config{AllowedTags: []string{"a", "b", "blockquote", "br", "code", "em", "i", "li", "ol", "p", "pre", "strong", "ul"}, InputBytes: MaxInputBytes, OutputBytes: MaxOutputBytes}
}

// Policy never exposes the mutable third-party builder. It is safe for concurrent
// calls after construction. No CSS, event handlers, data attributes, forms,
// embedded documents, SVG/MathML, data URLs or script-capable tags are admitted.
type Policy struct {
	policy                  *bluemonday.Policy
	inputBytes, outputBytes int
}

// New validates and snapshots configuration into a sanitization policy with
// restricted attributes and URL schemes.
func New(config Config) (*Policy, error) {
	if config.InputBytes < 1 || config.InputBytes > MaxInputBytes || config.OutputBytes < 1 || config.OutputBytes > MaxOutputBytes || len(config.AllowedTags) > 64 {
		return nil, invalid()
	}
	allowed := make([]string, 0, len(config.AllowedTags))
	seen := make(map[string]bool, len(config.AllowedTags))
	for _, raw := range config.AllowedTags {
		tag := strings.ToLower(strings.TrimSpace(raw))
		switch tag {
		case "a", "abbr", "b", "blockquote", "br", "caption", "code", "del", "div", "em", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "img", "li", "ol", "p", "pre", "s", "span", "strong", "sub", "sup", "table", "tbody", "td", "th", "thead", "tr", "u", "ul":
		default:
			return nil, invalid()
		}
		if seen[tag] {
			return nil, invalid()
		}
		seen[tag] = true
		allowed = append(allowed, tag)
	}
	schemes := []string{"https", "http", "mailto"}
	seenSchemes := make(map[URLScheme]bool)
	for _, scheme := range config.AdditionalURLSchemes {
		if scheme != URLSchemeTel || seenSchemes[scheme] {
			return nil, invalid()
		}
		seenSchemes[scheme] = true
		schemes = append(schemes, string(scheme))
	}
	p := bluemonday.NewPolicy()
	p.AllowElements(allowed...)
	p.SkipElementsContent("applet", "base", "embed", "frame", "frameset", "iframe", "link", "math", "meta", "object", "script", "style", "svg", "template")
	if seen["a"] {
		p.AllowAttrs("title").OnElements("a")
		if config.AllowRelativeURLs {
			p.AllowAttrs("href").Matching(localOrAbsoluteURL).OnElements("a")
		} else {
			p.AllowAttrs("href").OnElements("a")
		}
	}
	if seen["img"] {
		p.AllowAttrs("alt", "title").OnElements("img")
		if config.AllowRelativeURLs {
			p.AllowAttrs("src").Matching(localOrAbsoluteURL).OnElements("img")
		} else {
			p.AllowAttrs("src").OnElements("img")
		}
	}
	p.RequireParseableURLs(true).AllowURLSchemes(schemes...).AllowRelativeURLs(config.AllowRelativeURLs).RequireNoFollowOnLinks(true).RequireNoReferrerOnLinks(true)
	return &Policy{policy: p, inputBytes: config.InputBytes, outputBytes: config.OutputBytes}, nil
}

// Bluemonday treats //host as relative. Before its URL validation, require a
// non-slash/backslash first character, or a single leading slash. Match all
// strings.TrimSpace whitespace so padding cannot hide an authority prefix.
var localOrAbsoluteURL = regexp.MustCompile(`\A[[:space:]\p{Z}\x{0085}]*(?:[^/\\[:space:]\p{Z}\x{0085}]|/[^/\\[:space:]\p{Z}\x{0085}]|/[[:space:]\p{Z}\x{0085}]*\z)`)

// HTML sanitizes valid UTF-8 HTML within input and output budgets, honoring context cancellation.
func (p *Policy) HTML(ctx context.Context, input string) (string, error) {
	if p == nil || p.policy == nil || ctx == nil || len(input) > p.inputBytes || !utf8.ValidString(input) || strings.ContainsRune(input, 0) {
		return "", invalid()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	out := boundedOutput{ctx: ctx, remaining: p.outputBytes}
	err := p.policy.SanitizeReaderToWriter(contextInput{ctx: ctx, reader: strings.NewReader(input)}, &out)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return out.buffer.String(), nil
}

// StripTags returns escaped HTML text with all tags removed. Active-element
// content is discarded. Calls share one immutable empty-allowlist policy with
// the default bounds; it is built on first use.
func StripTags(ctx context.Context, input string) (string, error) {
	p, err := stripTagsPolicy()
	if err != nil {
		return "", err
	}
	return p.HTML(ctx, input)
}

// stripTagsPolicy is immutable after construction and safe for concurrent use;
// it is not configurable application state.
var stripTagsPolicy = sync.OnceValues(func() (*Policy, error) {
	config := DefaultConfig()
	config.AllowedTags = nil
	return New(config)
})

type contextInput struct {
	ctx    context.Context
	reader io.Reader
}

// Read checks context cancellation before reading input.
func (r contextInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type boundedOutput struct {
	ctx       context.Context
	remaining int
	buffer    bytes.Buffer
}

// Write writes output within the remaining byte budget after checking cancellation.
func (w *boundedOutput) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > w.remaining {
		return 0, invalid()
	}
	n, err := w.buffer.Write(p)
	w.remaining -= n
	return n, err
}
func invalid() error {
	return safeerror.NewFault(fault.Invalid, "invalid HTML sanitization policy, input or output bound")
}
