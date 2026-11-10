package http

import (
	"cmp"
	stdhttp "net/http"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/httppath"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CaptureDeclaration is an immutable prefix/body-policy declaration. Its zero
// value is invalid. Runtime settings may reduce, but never expand, this policy.
type CaptureDeclaration struct {
	prefix PathPrefix
	full   bool
}

// CaptureOption selects a declaration's body policy.
type CaptureOption struct{ full bool }

// FullCaptureBody explicitly opts a declared prefix into complete exact bodies.
// Header and query disclosure policy is independent of this selection.
func FullCaptureBody() CaptureOption { return CaptureOption{full: true} }

// CapturePrefix declares a literal decoded URL path prefix and its descendants.
// Invalid/root/duplicate declarations are rejected at server preparation.
func CapturePrefix(prefix PathPrefix, options ...CaptureOption) CaptureDeclaration {
	d := CaptureDeclaration{prefix: prefix}
	for _, option := range options {
		d.full = d.full || option.full
	}
	return d
}

type capturePolicy struct {
	recorder *httpcapture.Recorder
	prefixes []CaptureDeclaration
}

func (p *capturePolicy) validateRoutes(handler stdhttp.Handler) error {
	if p == nil {
		return nil
	}
	for _, route := range captureRouteInventory(handler) {
		if route.Authentication == nil {
			continue
		}
		segments, err := httppath.Parse(route.Path)
		if err != nil {
			return err
		}
		for _, declaration := range p.prefixes {
			if captureRouteOverlap(segments, declaration.prefix.normalized()) {
				return safeerror.NewFault(fault.Invalid, "authenticated route lies under an HTTP capture prefix")
			}
		}
	}
	return nil
}

// Compare the prefix against the route grammar, including parameter/tail
// segments: /{tenant}/callback can also accept /wh/callback.
func captureRouteOverlap(route []httppath.Segment, prefix string) bool {
	for i, part := range strings.Split(strings.TrimPrefix(prefix, "/"), "/") {
		if i >= len(route) {
			return false
		}
		segment := route[i]
		if segment.Tail {
			return true
		}
		if segment.Name == "" && segment.Literal != part {
			return false
		}
	}
	return true
}

func captureRouteInventory(handler stdhttp.Handler) []RouteInfo {
	if source, ok := handler.(interface{ captureRoutes() []RouteInfo }); ok {
		return source.captureRoutes()
	}
	return nil
}

// CredentialQueryName reports whether a query parameter name carries a
// framework bearer credential: a ticket or a signed-URL signature. Capture
// masks these values even though a signature header stays visible.
func CredentialQueryName(name string) bool {
	return name == ticketParameter || name == signedURLSignature
}

// MaxCapturePrefixes bounds the declarations one server matches per request.
const MaxCapturePrefixes = 32

// ValidateCaptureDeclarations rejects an empty or oversized set, the root
// prefix, zero declarations and duplicate prefixes. Application assembly uses
// it before any resource opens; WithWebhookCapture applies the same rule.
func ValidateCaptureDeclarations(declarations ...CaptureDeclaration) error {
	if len(declarations) == 0 || len(declarations) > MaxCapturePrefixes {
		return safeerror.NewFault(fault.Invalid, "HTTP capture requires 1 to 32 prefixes")
	}
	seen := make(map[string]bool, len(declarations))
	for _, declaration := range declarations {
		path := declaration.prefix.normalized()
		if declaration.prefix.path == "" || path == "" || path == "/" || seen[path] {
			return safeerror.NewFault(fault.Invalid, "HTTP capture has an invalid or duplicate prefix")
		}
		seen[path] = true
	}
	return nil
}

// WithWebhookCapture attaches the framework's static recorder, once, outside
// middleware. The internal recorder cannot be constructed by applications.
// Ordinary application assembly supplies it on behalf of Go declarations.
func WithWebhookCapture(recorder *httpcapture.Recorder, declarations ...CaptureDeclaration) ServerOption {
	declarations = slices.Clone(declarations)
	return func(o *serverOptions) error {
		if recorder == nil || o.capture != nil {
			return safeerror.NewFault(fault.Invalid, "HTTP capture requires one recorder")
		}
		if err := ValidateCaptureDeclarations(declarations...); err != nil {
			return err
		}
		prefixes := slices.Clone(declarations)
		slices.SortFunc(prefixes, func(a, b CaptureDeclaration) int {
			return cmp.Compare(len(b.prefix.normalized()), len(a.prefix.normalized()))
		})
		o.capture = &capturePolicy{recorder: recorder, prefixes: prefixes}
		return nil
	}
}

type kernelCapture struct {
	reservation *httpcapture.Reservation
	snapshot    httpcapture.Snapshot
	limit       int
	replay      *replayCaptureBody
	head        *captureResponse
}

func (p *capturePolicy) begin(r *stdhttp.Request, proxy *proxyPolicy, started time.Time) *kernelCapture {
	if p == nil || r.URL == nil || RequestID(r.Context()) == "" {
		return nil
	}
	var selected *CaptureDeclaration
	for i := range p.prefixes {
		if matchesAssetPrefix(r.URL.Path, p.prefixes[i].prefix.normalized()) {
			selected = &p.prefixes[i]
			break
		}
	}
	if selected == nil {
		return nil
	}
	lease := p.recorder.Reserve()
	if lease == nil {
		return nil
	}
	m := httpcapture.Metadata{RequestID: RequestID(r.Context()), ReceivedAt: started, Prefix: selected.prefix.normalized(), Full: selected.full, ClientIP: PeerIP(r)}
	if proxy != nil {
		m.ClientIP = proxy.clientIP(r)
	}
	m.Method, m.MethodTruncated = captureString(r.Method, 32)
	m.EscapedPath, m.PathTruncated = captureString(r.URL.EscapedPath(), 4<<10)
	m.RawQuery, m.QueryTruncated = captureString(r.URL.RawQuery, 16<<10)
	m.ContentType, m.ContentTypeTruncated = captureString(r.Header.Get("Content-Type"), 256)
	m.ContentEncoding, m.ContentEncodingTruncated = captureString(r.Header.Get("Content-Encoding"), 256)
	if r.ContentLength >= 0 {
		m.DeclaredBytes = value.Of(r.ContentLength)
	}
	m.Headers, m.HeadersTruncated = captureHeaders(r.Header)
	return &kernelCapture{reservation: lease, snapshot: httpcapture.Snapshot{Metadata: m}, limit: p.recorder.BodyBytes(), head: &captureResponse{suppressBody: r.Method == stdhttp.MethodHead}}
}

func captureString(text string, limit int) (string, bool) {
	return strings.Clone(text[:min(len(text), limit)]), len(text) > limit
}

func captureHeaders(header stdhttp.Header) ([]httpcapture.Field, bool) {
	const budget = 16 << 10
	// Keep only the first 128 canonical names while sorting, so the snapshot
	// and its truncation never depend on map order. An oversized native header
	// map must not allocate an unbudgeted full key inventory.
	type namePair struct{ canonical, original string }
	names := make([]namePair, 0, min(len(header), 128))
	truncated := false
	for original := range header {
		if len(original) > 256 {
			truncated = true
			continue
		}
		name := namePair{stdhttp.CanonicalHeaderKey(original), original}
		index, _ := slices.BinarySearchFunc(names, name, func(a, b namePair) int {
			if order := strings.Compare(a.canonical, b.canonical); order != 0 {
				return order
			}
			return strings.Compare(a.original, b.original)
		})
		if len(names) == 128 {
			truncated = true
			if index == 128 {
				continue
			}
			names = names[:127]
		}
		names = slices.Insert(names, index, name)
	}
	remaining := budget
	fields := make([]httpcapture.Field, 0, len(names))
	entries := 0
	for _, pair := range names {
		name, values := pair.canonical, header[pair.original]
		if len(fields) == 128 || remaining == 0 {
			truncated = true
			break
		}
		key, cut := captureString(name, min(256, remaining))
		remaining -= len(key)
		truncated = truncated || cut
		field := httpcapture.Field{Name: key}
		for _, raw := range values {
			if entries == 128 || remaining == 0 {
				truncated = true
				break
			}
			text, cut := captureString(raw, min(4<<10, remaining))
			field.Values = append(field.Values, text)
			entries++
			remaining -= len(text)
			truncated = truncated || cut
		}
		fields = append(fields, field)
	}
	return fields, truncated
}

func (c *kernelCapture) finish(observation *requestObservation, response *observedResponse) {
	if c == nil {
		return
	}
	s := c.snapshot
	if c.replay != nil {
		s.Body = c.replay.snapshot(c.limit)
	}
	result := observation.result(response)
	completion := httpcapture.Completion{Result: result, ResponseBytes: response.bytes, Hijacked: response.hijacked, Aborted: !observation.returned, WriteFailed: response.err != nil, Duration: time.Since(s.Metadata.ReceivedAt)}
	if matched := observation.route.Load(); matched != nil {
		completion.Route = string(matched.info.ID)
	}
	status := response.status
	if status == 0 && observation.returned && !response.hijacked {
		status = stdhttp.StatusOK
	}
	if status != 0 {
		completion.Status = value.Of(uint16(status))
	}
	completion.ResponseHead = c.head.prefix
	completion.ResponseTruncated = c.head.truncated
	completion.ResponseContentType = c.head.contentType
	completion.ResponseEncoding = c.head.encoding
	completion.ResponseMetadataTruncated = c.head.metadataTruncated
	s.Completion = completion
	c.reservation.Complete(s)
	// No payload remains owned by the request while extension observers run.
	c.snapshot = httpcapture.Snapshot{}
	c.head.prefix = nil
}
