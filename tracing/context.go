// Package tracing preserves bounded W3C trace context without global providers,
// recording backends, request payloads or authentication capabilities.
package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

const ParentHeader = "traceparent"
const StateHeader = "tracestate"
const MaxParentBytes = 512
const MaxStateBytes = 512

type TraceID [16]byte
type SpanID [8]byte

func (id TraceID) String() string               { return hex.EncodeToString(id[:]) }
func (id SpanID) String() string                { return hex.EncodeToString(id[:]) }
func (id TraceID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }
func (id SpanID) MarshalText() ([]byte, error)  { return []byte(id.String()), nil }

// Text decoding supports owned diagnostics snapshots, including a zero parent
// ID for root spans. Propagation Context.Validate separately rejects zero IDs.
func (id *TraceID) UnmarshalText(text []byte) error {
	if id == nil || len(text) != 32 || !lowerHex(string(text)) {
		return fault.New(fault.Invalid, "invalid trace ID text")
	}
	var decoded TraceID
	if _, err := hex.Decode(decoded[:], text); err != nil {
		return fault.New(fault.Invalid, "invalid trace ID text")
	}
	*id = decoded
	return nil
}
func (id *SpanID) UnmarshalText(text []byte) error {
	if id == nil || len(text) != 16 || !lowerHex(string(text)) {
		return fault.New(fault.Invalid, "invalid span ID text")
	}
	var decoded SpanID
	if _, err := hex.Decode(decoded[:], text); err != nil {
		return fault.New(fault.Invalid, "invalid span ID text")
	}
	*id = decoded
	return nil
}
func (id TraceID) IsZero() bool { return id == TraceID{} }
func (id SpanID) IsZero() bool  { return id == SpanID{} }

// Context is an immutable propagation snapshot. Trace IDs correlate operations,
// not users; do not infer authentication or authorization from trace metadata.
// Tracestate is opaque vendor data and is omitted from ordinary formatting.
type Context struct {
	trace   TraceID
	span    SpanID
	sampled bool
	state   string
}

func (c Context) TraceID() TraceID   { return c.trace }
func (c Context) SpanID() SpanID     { return c.span }
func (c Context) Sampled() bool      { return c.sampled }
func (c Context) TraceState() string { return c.state }
func (c Context) IsZero() bool {
	return c.trace.IsZero() && c.span.IsZero() && !c.sampled && c.state == ""
}
func (Context) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("trace context")) }

// LogValue keeps opaque vendor state out of structured logs even though an
// explicit MarshalJSON propagation snapshot includes it.
func (c Context) LogValue() slog.Value {
	return slog.GroupValue(slog.String("trace_id", c.trace.String()), slog.String("span_id", c.span.String()), slog.Bool("sampled", c.sampled))
}

func (c Context) Validate() error {
	if c.trace.IsZero() || c.span.IsZero() {
		return fault.New(fault.Invalid, "trace context requires nonzero trace and span IDs")
	}
	return nil
}

func randomID(bytes []byte) error {
	// An all-zero value is forbidden by the propagation format. Bound the
	// astronomically unlikely retry instead of silently manufacturing an ID.
	for range 4 {
		if _, err := rand.Read(bytes); err != nil {
			return fault.Wrap(fault.Internal, "cannot create trace identity", err)
		}
		for _, b := range bytes {
			if b != 0 {
				return nil
			}
		}
	}
	return fault.New(fault.Internal, "cannot create nonzero trace identity")
}

func New(sampled bool) (Context, error) {
	c := Context{sampled: sampled}
	if err := randomID(c.trace[:]); err != nil {
		return Context{}, err
	}
	if err := randomID(c.span[:]); err != nil {
		return Context{}, err
	}
	return c, nil
}

// Child creates a new operation in the same trace. Its explicit sampling choice
// is local policy, not a requirement imposed by an untrusted caller's flag.
func (c Context) Child(sampled bool) (Context, error) {
	if err := c.Validate(); err != nil {
		return Context{}, err
	}
	if err := randomID(c.span[:]); err != nil {
		return Context{}, err
	}
	c.sampled = sampled
	return c, nil
}

// TraceParent emits the supported version 00 representation, retaining only
// the defined sampled flag. Zero context emits no propagation header.
func (c Context) TraceParent() string {
	if c.Validate() != nil {
		return ""
	}
	flags := "00"
	if c.sampled {
		flags = "01"
	}
	return "00-" + c.trace.String() + "-" + c.span.String() + "-" + flags
}

func lowerHex(text string) bool {
	for i := range len(text) {
		if !(text[i] >= '0' && text[i] <= '9' || text[i] >= 'a' && text[i] <= 'f') {
			return false
		}
	}
	return true
}

// Parse accepts traceparent version 00 and the known prefix of future versions.
// Malformed parents fail without retaining their values. Invalid tracestate is
// discarded independently, as required by the W3C processing model. Adapters
// choose whether to trust propagation at their network boundary.
func Parse(parent, state string) (Context, error) {
	invalid := func() (Context, error) { return Context{}, fault.New(fault.Invalid, "invalid traceparent") }
	if len(parent) < 55 || len(parent) > MaxParentBytes || parent[2] != '-' || parent[35] != '-' || parent[52] != '-' || !lowerHex(parent[:2]) || parent[:2] == "ff" {
		return invalid()
	}
	if parent[:2] == "00" && len(parent) != 55 || len(parent) > 55 && parent[55] != '-' {
		return invalid()
	}
	if !lowerHex(parent[3:35]) || !lowerHex(parent[36:52]) || !lowerHex(parent[53:55]) {
		return invalid()
	}
	c := Context{}
	_, _ = hex.Decode(c.trace[:], []byte(parent[3:35]))
	_, _ = hex.Decode(c.span[:], []byte(parent[36:52]))
	var flags [1]byte
	_, _ = hex.Decode(flags[:], []byte(parent[53:55]))
	c.sampled = flags[0]&1 != 0
	if err := c.Validate(); err != nil {
		return invalid()
	}
	if normalized, err := normalizeState(state); err == nil {
		c.state = normalized
	}
	return c, nil
}

// WithState replaces vendor metadata after validation. Call Child before
// modifying propagation; opaque vendor data must not enter ordinary logs.
func (c Context) WithState(state string) (Context, error) {
	if err := c.Validate(); err != nil {
		return Context{}, err
	}
	normalized, err := normalizeState(state)
	if err != nil {
		return Context{}, err
	}
	c.state = normalized
	return c, nil
}

type contextKey struct{}

func FromContext(ctx context.Context) Context {
	if ctx == nil {
		return Context{}
	}
	value, _ := ctx.Value(contextKey{}).(Context)
	return value
}

// WithoutContext starts a new trace boundary while preserving cancellation and
// all unrelated context values. HTTP ingress uses it to prevent a long-lived
// kernel span from becoming the parent of every independent incoming request.
func WithoutContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, Context{})
}

func WithContext(ctx context.Context, value Context) (context.Context, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "trace attachment requires a context")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, contextKey{}, value), nil
}

// Start derives one child operation, or starts a new root when the context has
// no trace. It preserves the caller's cancellation, deadlines and other values.
func Start(ctx context.Context, sampled bool) (context.Context, Context, error) {
	if ctx == nil {
		return nil, Context{}, fault.New(fault.Invalid, "trace start requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, Context{}, err
	}
	parent := FromContext(ctx)
	var next Context
	var err error
	if parent.IsZero() {
		next, err = New(sampled)
	} else {
		next, err = parent.Child(sampled)
	}
	if err != nil {
		return nil, Context{}, err
	}
	attached, err := WithContext(ctx, next)
	return attached, next, err
}

type wireContext struct {
	Parent string `json:"traceparent"`
	State  string `json:"tracestate,omitempty"`
}

// MarshalJSON is an explicit propagation boundary and includes vendor state.
// An absent trace should be omitted by its enclosing versioned envelope.
func (c Context) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(wireContext{Parent: c.TraceParent(), State: c.state})
}

func (c *Context) UnmarshalJSON(data []byte) error {
	if c == nil {
		return fault.New(fault.Invalid, "trace decoding requires a destination")
	}
	decoded, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: 2048, Depth: 2, Nodes: 8})
	if err != nil {
		return fault.New(fault.Invalid, "invalid trace context document")
	}
	object, ok := decoded.(map[string]any)
	if !ok || len(object) < 1 || len(object) > 2 {
		return fault.New(fault.Invalid, "invalid trace context document")
	}
	parent, ok := object["traceparent"].(string)
	if !ok {
		return fault.New(fault.Invalid, "trace context requires traceparent")
	}
	state := ""
	if raw, present := object["tracestate"]; present {
		var valid bool
		state, valid = raw.(string)
		if !valid {
			return fault.New(fault.Invalid, "invalid tracestate value")
		}
	}
	for key := range object {
		if key != "traceparent" && key != "tracestate" {
			return fault.New(fault.Invalid, "unknown trace context field")
		}
	}
	if _, err := normalizeState(state); err != nil {
		return err
	}
	next, err := Parse(parent, state)
	if err != nil {
		return err
	}
	*c = next
	return nil
}
