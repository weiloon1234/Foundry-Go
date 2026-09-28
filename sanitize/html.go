// Package sanitize supplies immutable HTML fragment allowlists backed by
// bluemonday. Output is for HTML body content, not scripts, CSS, URLs or attributes.
package sanitize

import (
	"bytes"
	"context"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"github.com/weiloon1234/Foundry-Go/fault"
)

const MaxInputBytes = 1 << 20
const MaxOutputBytes = 4 << 20

type Config struct {
	AllowedTags []string
	InputBytes  int
	OutputBytes int
}

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
	p := bluemonday.NewPolicy()
	p.AllowElements(allowed...)
	p.SkipElementsContent("applet", "base", "embed", "frame", "frameset", "iframe", "link", "math", "meta", "object", "script", "style", "svg", "template")
	if seen["a"] {
		p.AllowAttrs("href", "title").OnElements("a")
	}
	if seen["img"] {
		p.AllowAttrs("src", "alt", "title").OnElements("img")
	}
	p.RequireParseableURLs(true).AllowURLSchemes("https", "http", "mailto").AllowRelativeURLs(false).RequireNoFollowOnLinks(true).RequireNoReferrerOnLinks(true)
	return &Policy{policy: p, inputBytes: config.InputBytes, outputBytes: config.OutputBytes}, nil
}

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
// content is discarded. Reuse New with an empty allowlist for repeated calls.
func StripTags(ctx context.Context, input string) (string, error) {
	config := DefaultConfig()
	config.AllowedTags = nil
	p, err := New(config)
	if err != nil {
		return "", err
	}
	return p.HTML(ctx, input)
}

type contextInput struct {
	ctx    context.Context
	reader io.Reader
}

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
	return fault.New(fault.Invalid, "invalid HTML sanitization policy, input or output bound")
}
