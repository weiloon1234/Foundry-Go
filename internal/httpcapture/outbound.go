package httpcapture

import (
	"context"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/tracing"
	"github.com/weiloon1234/Foundry-Go/value"
)

// OutboundMetadataBytes is the allowance one outbound exchange reserves for
// its bounded URL, headers and bookkeeping. Body buffers grow on demand.
const OutboundMetadataBytes = 64 << 10

// OutboundLaneShare caps one client at a quarter of the process queue and
// retained bytes, so at least four clients always make progress.
const OutboundLaneShare = 4

const (
	tapStart     = 32 << 10
	maxPathBytes = 4 << 10
	maxQuery     = 16 << 10
)

// OutboundSinks is the contribution collection httpclient.Module reads. Its
// element type is internal, so only framework assembly contributes to it.
var OutboundSinks = foundation.NewCollection[*Outbound]("foundry.outbound-capture")

// Rule is one validated exclusion. Empty fields match anything.
type Rule struct {
	Client string
	// Host is lowercase; "*.x" matches any subdomain of x, not x itself.
	Host   string
	Method string
	// PathPrefix is a normalized decoded prefix without a trailing slash; it
	// matches segment by segment.
	PathPrefix string
}

func (r Rule) clientOnly() bool {
	return r.Client != "" && r.Host == "" && r.Method == "" && r.PathPrefix == ""
}

func (r Rule) matches(method string, u *url.URL) bool {
	if r.Method != "" && r.Method != method {
		return false
	}
	if r.Host != "" {
		host := strings.ToLower(u.Hostname())
		if suffix, wildcard := strings.CutPrefix(r.Host, "*"); wildcard {
			if !strings.HasSuffix(host, suffix) || len(host) == len(suffix) {
				return false
			}
		} else if host != r.Host {
			return false
		}
	}
	return r.PathPrefix == "" || u.Path == r.PathPrefix || strings.HasPrefix(u.Path, r.PathPrefix+"/")
}

// OutboundConfig is the validated policy one application's recorder applies.
type OutboundConfig struct {
	Limits Config
	// Full names the clients whose bodies are stored exactly.
	Full  []string
	Rules []Rule
	// Inspect decides from Content-Type and Content-Encoding whether redacted
	// mode can store a body; the disclosure owner supplies it.
	Inspect func(contentType, contentEncoding string) Skip
}

// Outbound is the static, I/O-free recorder for client attempts, built once
// per application. Each client name gets one lane at construction.
type Outbound struct {
	recorder *Recorder[Exchange]
	limit    int
	full     map[string]bool
	rules    []Rule
	inspect  func(contentType, contentEncoding string) Skip
	mu       sync.Mutex
	lanes    map[string]*ClientLane
	claimed  map[string]bool
}

func NewOutbound(c OutboundConfig) (*Outbound, error) {
	if c.Inspect == nil || c.Limits.LaneShare != OutboundLaneShare {
		return nil, safeerror.NewFault(fault.Invalid, "invalid outbound capture recorder")
	}
	recorder, err := New[Exchange](c.Limits)
	if err != nil {
		return nil, err
	}
	o := &Outbound{recorder: recorder, limit: c.Limits.BodyBytes, full: make(map[string]bool, len(c.Full)), rules: append([]Rule(nil), c.Rules...), inspect: c.Inspect, lanes: make(map[string]*ClientLane), claimed: make(map[string]bool)}
	for _, name := range c.Full {
		o.full[name] = true
	}
	return o, nil
}

// Recorder is the queue the outbound store drains.
func (o *Outbound) Recorder() *Recorder[Exchange] { return o.recorder }

// Lane returns the client's capture lane, or nil when a client-only rule
// excludes it. Clients of the same name share one lane.
func (o *Outbound) Lane(name string) *ClientLane {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.claimed[name] = true
	if lane, ok := o.lanes[name]; ok {
		return lane
	}
	var rules []Rule
	for _, rule := range o.rules {
		if rule.Client != "" && rule.Client != name {
			continue
		}
		if rule.clientOnly() {
			o.lanes[name] = nil
			return nil
		}
		rules = append(rules, rule)
	}
	lane := &ClientLane{outbound: o, name: name, full: o.full[name], rules: rules, admit: o.recorder.NewLane()}
	o.lanes[name] = lane
	return lane
}

// Claimed reports whether a client of this name was constructed.
func (o *Outbound) Claimed(name string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.claimed[name]
}

// ClientLane is one client's capture policy and admission lane.
type ClientLane struct {
	outbound *Outbound
	name     string
	full     bool
	rules    []Rule
	admit    *Lane[Exchange]
}

// RequestFacts are the immutable request pieces httpclient hands over.
type RequestFacts struct {
	Method  string
	URL     *url.URL
	Headers http.Header
	// Data is a Bytes body's private, immutable copy; nil for no body or a stream.
	Data     []byte
	Stream   bool
	Length   int64
	Declined bool
}

// Cause links a call to the request that made it.
type Cause struct {
	RequestID attribution.RequestID
	Trace     tracing.TraceID
	// Route is the matched inbound route's declared name.
	Route string
}

// Operation is one Do, Stream or Download call. Methods on a nil operation
// are no-ops, so httpclient needs no branches.
type Operation struct {
	lane    *ClientLane
	id      [16]byte
	cause   Cause
	request RequestFacts
}

// Begin starts one operation, or returns nil when the client is not captured
// or a rule excludes the call. A nil URL is never captured.
func (l *ClientLane) Begin(ctx context.Context, request RequestFacts) *Operation {
	if l == nil || request.URL == nil {
		return nil
	}
	for _, rule := range l.rules {
		if rule.matches(request.Method, request.URL) {
			return nil
		}
	}
	id, err := model.NewIDAt[Exchange](time.Now())
	if err != nil {
		return nil
	}
	cause := Cause{RequestID: attribution.FromContext(ctx).Request().ID}
	if trace := tracing.FromContext(ctx); !trace.IsZero() {
		cause.Trace = trace.TraceID()
	}
	if route, ok := attribution.RouteFromContext(ctx); ok {
		cause.Route = route.Name
	}
	return &Operation{lane: l, id: id.Bytes(), cause: cause, request: request}
}

// Result is the closed per-attempt outcome; the public enum mirrors it.
type Result uint8

const (
	ResultCompleted Result = iota
	ResultConsumerFailed
	ResultResponseBodyFailed
	ResultResponseTooLarge
	ResultRequestBodyFailed
	ResultTransportFailed
	ResultTimedOut
	ResultCanceled
	ResultDestinationDenied
	ResultOverloaded
)

// Skip says why a body's bytes were not kept.
type Skip uint8

const (
	Kept Skip = iota
	// SkipNotSent: the operation never reached the wire (attempt 0).
	SkipNotSent
	// SkipMedia: redacted mode cannot inspect this media type.
	SkipMedia
	// SkipEncoded: the body is content-encoded in redacted mode.
	SkipEncoded
	// SkipDeclined: Request.WithoutBodyCapture.
	SkipDeclined
	// SkipMemory: the retained budget refused the body buffer.
	SkipMemory
)

// BodyCapture is one direction's raw body facts.
type BodyCapture struct {
	// Bytes is an owned tap buffer or, for a request Bytes body within the
	// limit, the body's immutable copy.
	Bytes []byte
	// Declared is the Content-Length; null when unknown.
	Declared value.Nullable[int64]
	// Transferred counts bytes the transport sent or the caller read.
	Transferred int64
	// Complete means the end of the body was observed.
	Complete bool
	// Over means more than the capture limit passed.
	Over            bool
	Skip            Skip
	ContentType     string
	ContentEncoding string
}

// Exchange is one attempt's raw, bounded snapshot. Disclosure happens later
// in the writer; formatting never prints it.
type Exchange struct {
	ID, Operation      [16]byte
	Client             string
	Attempt            uint8
	Retried, Full      bool
	StartedAt          time.Time
	HeadersAfter       value.Nullable[time.Duration]
	Duration           time.Duration
	Cause              Cause
	Method             string
	Scheme, Host       string
	EscapedPath        string
	RawQuery           string
	PathCut, QueryCut  bool
	RequestHeaders     []Field
	RequestHeadersCut  bool
	Request            BodyCapture
	Status             value.Nullable[uint16]
	Protocol           string
	RemoteIP           netip.Addr
	Reused             value.Nullable[bool]
	ResponseHeaders    []Field
	ResponseHeadersCut bool
	Response           BodyCapture
	Result             Result
}

// NotSent stores an attempt-0 row: the client's admission was full or the
// URL's destination was refused before anything reached the wire.
func (o *Operation) NotSent(result Result) {
	a := o.Attempt(0)
	if a == nil {
		return
	}
	a.url(o.request.URL)
	a.snapshot.RequestHeaders, a.snapshot.RequestHeadersCut = Headers(o.request.Headers)
	a.snapshot.Request = BodyCapture{Skip: SkipNotSent}
	a.finish(false, result)
}

// Attempt reserves one exchange, or returns nil when the client's lane
// refuses admission.
func (o *Operation) Attempt(number int) *Attempt {
	if o == nil {
		return nil
	}
	now := time.Now()
	id, err := model.NewIDAt[Exchange](now)
	if err != nil {
		return nil
	}
	reservation := o.lane.admit.Reserve(OutboundMetadataBytes)
	if reservation == nil {
		return nil
	}
	method, _ := Clip(o.request.Method, 32)
	a := &Attempt{reservation: reservation, operation: o, limit: o.lane.outbound.limit}
	a.snapshot = Exchange{ID: id.Bytes(), Operation: o.id, Client: o.lane.name, Attempt: uint8(number), Full: o.lane.full, StartedAt: now, Cause: o.cause, Method: method}
	return a
}

// Attempt is the producer-side handle of one exchange. Its methods run on the
// attempt's goroutine; a nil attempt ignores them.
type Attempt struct {
	reservation *Reservation[Exchange]
	operation   *Operation
	limit       int
	snapshot    Exchange
	data        bool
	request     Tap
	response    Tap
	connMu      sync.Mutex
	conn        bool
	remote      netip.Addr
	reused      bool
}

// Trace attaches a connection observer to the native request's context only,
// so a consumer callback never inherits it.
func (a *Attempt) Trace(ctx context.Context) context.Context {
	if a == nil {
		return ctx
	}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: a.gotConn})
}

func (a *Attempt) gotConn(info httptrace.GotConnInfo) {
	var remote netip.Addr
	if info.Conn != nil {
		if addr := info.Conn.RemoteAddr(); addr != nil {
			if parsed, err := netip.ParseAddrPort(addr.String()); err == nil {
				remote = parsed.Addr().Unmap()
			}
		}
	}
	a.connMu.Lock()
	defer a.connMu.Unlock()
	if !a.conn {
		a.conn, a.remote, a.reused = true, remote, info.Reused
	}
}

// RequestTap decides, before the first byte, how the request body is kept.
// A Bytes body is kept by reference instead and needs no tap.
func (a *Attempt) RequestTap() *Tap {
	if a == nil {
		return nil
	}
	request := a.operation.request
	contentType, encoding := request.Headers.Get("Content-Type"), request.Headers.Get("Content-Encoding")
	b := &a.snapshot.Request
	b.ContentType, _ = Clip(contentType, 256)
	b.ContentEncoding, _ = Clip(encoding, 256)
	if request.Length >= 0 && (request.Stream || len(request.Data) > 0) {
		b.Declared = value.Of(request.Length)
	}
	skip := a.skip(request.Declined, contentType, encoding)
	if !request.Stream {
		a.keepData(request.Data, skip)
		return nil
	}
	a.request.start(a.reservation, a.limit, skip, request.Length)
	return &a.request
}

func (a *Attempt) skip(declined bool, contentType, encoding string) Skip {
	switch {
	case declined:
		return SkipDeclined
	case a.snapshot.Full:
		return Kept
	default:
		return a.operation.lane.outbound.inspect(contentType, encoding)
	}
}

// keepData references an immutable Bytes body within the limit, or copies a
// full-mode prefix, so a large backing array is never pinned.
func (a *Attempt) keepData(data []byte, skip Skip) {
	b := &a.snapshot.Request
	a.data = len(data) > 0
	b.Skip, b.Complete = skip, true
	if !a.data || skip != Kept {
		return
	}
	if len(data) > a.limit {
		b.Over = true
		if !a.snapshot.Full {
			return
		}
		data = data[:a.limit:a.limit]
		if !a.reservation.Grow(int64(a.limit)) {
			b.Skip = SkipMemory
			return
		}
		b.Bytes = append([]byte(nil), data...)
		return
	}
	if !a.reservation.Grow(int64(len(data))) {
		b.Skip = SkipMemory
		return
	}
	b.Bytes = data
}

// Sending records the final native request: URL and headers after defaults
// and trace propagation.
func (a *Attempt) Sending(native *http.Request) {
	if a == nil || native == nil {
		return
	}
	a.url(native.URL)
	a.snapshot.RequestHeaders, a.snapshot.RequestHeadersCut = Headers(native.Header)
}

func (a *Attempt) url(u *url.URL) {
	if u == nil {
		return
	}
	s := &a.snapshot
	s.Scheme, _ = Clip(u.Scheme, 16)
	s.Host, _ = Clip(u.Host, 300)
	s.EscapedPath, s.PathCut = Clip(u.EscapedPath(), maxPathBytes)
	s.RawQuery, s.QueryCut = Clip(u.RawQuery, maxQuery)
}

// Received records the response head and decides how its body is kept.
// expected is the body length the client will enforce, or -1.
func (a *Attempt) Received(response *http.Response, expected int64) {
	if a == nil || response == nil || response.StatusCode < 100 || response.StatusCode > 599 {
		return
	}
	s := &a.snapshot
	s.HeadersAfter = value.Of(time.Since(s.StartedAt))
	s.Status = value.Of(uint16(response.StatusCode))
	s.Protocol, _ = Clip(response.Proto, 16)
	s.ResponseHeaders, s.ResponseHeadersCut = Headers(response.Header)
	contentType, encoding := response.Header.Get("Content-Type"), response.Header.Get("Content-Encoding")
	b := &s.Response
	b.ContentType, _ = Clip(contentType, 256)
	b.ContentEncoding, _ = Clip(encoding, 256)
	if expected >= 0 {
		b.Declared = value.Of(expected)
	}
	a.response.start(a.reservation, a.limit, a.skip(a.operation.request.Declined, contentType, encoding), expected)
}

// ResponseTap is the tap Received prepared; nil before a valid response head.
func (a *Attempt) ResponseTap() *Tap {
	if a == nil || a.snapshot.Status.IsNull() {
		return nil
	}
	return &a.response
}

// Finish runs once in the attempt's deferred cleanup after both bodies are
// closed, so their taps are quiescent. sent and read are the owned streams'
// byte counts.
func (a *Attempt) Finish(retried bool, result Result, sent, read int64) {
	if a == nil {
		return
	}
	s := &a.snapshot
	switch {
	case a.request.started:
		s.Request = a.request.settle(s.Request, sent)
	case a.data:
		s.Request.Transferred = sent
	case a.operation.request.Stream:
		// The stream never opened, so its end was never observed.
		s.Request = BodyCapture{Declared: s.Request.Declared, Transferred: sent}
	default:
		s.Request = BodyCapture{Complete: true, Transferred: sent}
	}
	if a.response.started {
		s.Response = a.response.settle(s.Response, read)
	}
	a.connMu.Lock()
	if a.conn {
		s.RemoteIP, s.Reused = a.remote, value.Of(a.reused)
	}
	a.connMu.Unlock()
	s.Duration = time.Since(s.StartedAt)
	a.finish(retried, result)
}

func (a *Attempt) finish(retried bool, result Result) {
	s := &a.snapshot
	s.Retried, s.Result = retried, result
	retained := int64(OutboundMetadataBytes + cap(s.Request.Bytes) + cap(s.Response.Bytes))
	a.reservation.Complete(*s, retained)
	a.snapshot, a.request, a.response = Exchange{}, Tap{}, Tap{}
}

// TapEnd is how an observed stream ended.
type TapEnd uint8

const (
	// TapOpen: closed before its end, or never read.
	TapOpen TapEnd = iota
	TapEOF
	// TapBound: the stream exceeded its byte bound.
	TapBound
	// TapFailed: a read failed.
	TapFailed
)

// Tap observes one owned stream. The stream calls Observe and End under its
// read lock, so a tap needs no lock of its own; Finish reads it after Close.
type Tap struct {
	reservation *Reservation[Exchange]
	started     bool
	limit       int
	keep        bool
	declared    int64
	buffer      []byte
	over        bool
	end         TapEnd
	skip        Skip
}

func (t *Tap) start(reservation *Reservation[Exchange], limit int, skip Skip, declared int64) {
	*t = Tap{reservation: reservation, started: true, limit: limit, keep: skip == Kept, skip: skip, declared: declared}
}

// Observe keeps up to the capture limit of the bytes a read returned. The
// first buffer is the declared length when known, else 32 KiB, with one
// growth to the limit; a refused charge turns the tap count-only.
func (t *Tap) Observe(p []byte) {
	if t == nil || !t.keep || len(p) == 0 {
		return
	}
	room := t.limit - len(t.buffer)
	if len(p) > room {
		t.over = true
		p = p[:room]
	}
	if len(p) == 0 {
		return
	}
	if need := len(t.buffer) + len(p); need > cap(t.buffer) {
		size := t.limit
		if cap(t.buffer) == 0 {
			if t.declared >= 0 {
				size = min(int(min(t.declared, int64(t.limit))), t.limit)
			} else {
				size = min(tapStart, t.limit)
			}
			size = max(size, need)
		}
		if !t.reservation.Grow(int64(size - cap(t.buffer))) {
			t.keep, t.skip, t.buffer = false, SkipMemory, nil
			return
		}
		grown := make([]byte, len(t.buffer), size)
		copy(grown, t.buffer)
		t.buffer = grown
	}
	t.buffer = append(t.buffer, p...)
}

// End records how the stream ended; only the first end counts.
func (t *Tap) End(end TapEnd) {
	if t != nil && t.end == TapOpen {
		t.end = end
	}
}

func (t *Tap) settle(b BodyCapture, transferred int64) BodyCapture {
	b.Bytes, b.Over, b.Skip = t.buffer, t.over || transferred > int64(t.limit), t.skip
	b.Transferred, b.Complete = transferred, t.end == TapEOF
	return b
}
