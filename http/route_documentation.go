package http

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Documentation bounds. Summary and tags are single lines; a description may
// contain line breaks and tabs.
const (
	MaxRouteSummaryBytes     = 256
	MaxRouteDescriptionBytes = 16 << 10
	MaxRouteTags             = 16
	MaxRouteTagBytes         = 64
)

// RouteDocumentation is human-facing API documentation exported with client
// contracts, OpenAPI and TypeScript comments. It never changes matching,
// access, limits or runtime behavior; the route ID remains the operation ID.
type RouteDocumentation struct {
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Deprecated  bool     `json:"deprecated,omitempty"`
}

// Validate checks text bounds and rejects empty or duplicate tags.
func (d RouteDocumentation) Validate() error {
	invalid := func(message string) error { return fault.New(fault.Invalid, "route documentation "+message) }
	if len(d.Summary) > MaxRouteSummaryBytes || !documentationText(d.Summary, false) {
		return invalid("summary must be one line of valid text within MaxRouteSummaryBytes")
	}
	if len(d.Description) > MaxRouteDescriptionBytes || !documentationText(d.Description, true) {
		return invalid("description must be valid text within MaxRouteDescriptionBytes")
	}
	if len(d.Tags) > MaxRouteTags {
		return invalid("tags exceed MaxRouteTags")
	}
	for i, tag := range d.Tags {
		if tag == "" || strings.TrimSpace(tag) != tag || len(tag) > MaxRouteTagBytes || !documentationText(tag, false) || slices.Contains(d.Tags[:i], tag) {
			return invalid("tags must be unique, trimmed single-line names within MaxRouteTagBytes")
		}
	}
	return nil
}

func documentationText(text string, multiline bool) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if multiline && (r == '\n' || r == '\t') {
			continue
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return false
		}
	}
	return true
}

func (d *RouteDocumentation) clone() *RouteDocumentation {
	if d == nil {
		return nil
	}
	result := *d
	result.Tags = slices.Clone(d.Tags)
	return &result
}

// WithDocumentation returns a route documented with a copy of documentation.
// A later call replaces the earlier documentation.
func (r Route[P]) WithDocumentation(documentation RouteDocumentation) Route[P] {
	if r.err == nil {
		if err := documentation.Validate(); err != nil {
			r.err = err
		} else {
			r.documentation = documentation.clone()
		}
	}
	return r
}

// WithDocumentation documents the endpoint's route; see Route.WithDocumentation.
func (e Endpoint[P, Q, B, R]) WithDocumentation(documentation RouteDocumentation) Endpoint[P, Q, B, R] {
	e.route = e.route.WithDocumentation(documentation)
	return e
}

// MaxExampleBytes bounds one encoded request or response example.
const MaxExampleBytes = 64 << 10

type endpointExamples struct {
	body, response json.RawMessage
	err            error
}

// WithBodyExample documents an example JSON request body. It is encoded now
// through the endpoint's own body contract, so it cannot disagree with the
// declared schema; the endpoint retains only the encoded bytes.
func (e Endpoint[P, Q, B, R]) WithBodyExample(body B) Endpoint[P, Q, B, R] {
	if e.body.kind != payloadJSON {
		e.examples.err = fault.New(fault.Invalid, "a body example requires a JSON request body")
		return e
	}
	e.examples.body, e.examples.err = encodeExample(e.examples.err, func(ctx context.Context) ([]byte, error) {
		return e.body.json.Encode(ctx, body, exampleLimits(e.limits.Body))
	})
	return e
}

// WithResponseExample documents an example JSON response, encoded through the
// endpoint's own response contract like WithBodyExample.
func (e Endpoint[P, Q, B, R]) WithResponseExample(response R) Endpoint[P, Q, B, R] {
	if e.response.kind != payloadJSON {
		e.examples.err = fault.New(fault.Invalid, "a response example requires a JSON response")
		return e
	}
	e.examples.response, e.examples.err = encodeExample(e.examples.err, func(ctx context.Context) ([]byte, error) {
		return e.response.json.Encode(ctx, response, exampleLimits(e.limits.Response))
	})
	return e
}

func encodeExample(previous error, encode func(context.Context) ([]byte, error)) (json.RawMessage, error) {
	if previous != nil {
		return nil, previous
	}
	// Encoding performs no I/O; a declaration has no request context.
	data, err := encode(context.Background())
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "endpoint example does not satisfy its contract", err)
	}
	return data, nil
}

// exampleLimits keeps the endpoint's own depth and work limits while bounding
// the encoded example's size.
func exampleLimits(limits contract.JSONLimits) contract.JSONLimits {
	limits.Bytes = min(limits.Bytes, MaxExampleBytes)
	return limits
}
