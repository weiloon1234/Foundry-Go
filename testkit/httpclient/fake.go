// Package httpclient supplies deterministic outbound HTTP transports for tests.
// A fake never falls through to the network. Recorded data is explicitly private
// test data; ordinary formatting omits URLs, headers and bodies.
package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const MaxRequests = 1024
const MaxRecordedBodyBytes = 1 << 20
const MaxRecordedBytes = 16 << 20
const MaxRecordedURLBytes = 16 << 10

var Exhausted = fault.New(fault.Missing, "outbound HTTP fake has no remaining outcome")

// Unmatched reports a request that matches no Route of a routed fake.
var Unmatched = fault.New(fault.Missing, "outbound HTTP fake has no matching route")

// Outcome is a fake HTTP response or transport failure.
type Outcome struct {
	status  int
	headers http.Header
	body    []byte
	failure error
}

// Respond copies headers and body into a response outcome, returning a zero outcome if recording
// bounds are exceeded.
func Respond(status int, headers http.Header, body []byte) Outcome {
	if len(body) > MaxRecordedBodyBytes || headerBytes(headers) < 0 {
		return Outcome{}
	}
	return Outcome{status: status, headers: headers.Clone(), body: slices.Clone(body)}
}

// Fail creates a fake outcome containing the supplied error.
func Fail(err error) Outcome { return Outcome{failure: err} }

// Format writes a fixed label without exposing outcome data.
func (Outcome) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("HTTP fake outcome")) }

// Request is a recorded outbound HTTP request.
type Request struct {
	method, url string
	headers     http.Header
	body        []byte
}

// Method returns the recorded HTTP method.
func (r Request) Method() string { return r.method }

// URL returns the recorded URL.
func (r Request) URL() string { return r.url }

// Headers returns a copy of the recorded headers.
func (r Request) Headers() http.Header { return r.headers.Clone() }

// Body returns a copy of the recorded body.
func (r Request) Body() []byte { return slices.Clone(r.body) }

// Format writes a fixed label without exposing request data.
func (Request) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("recorded HTTP request")) }

// Route answers requests whose method and URL match, in declaration order of
// its outcomes. The pattern uses path.Match syntax against "host/path" (scheme
// and query excluded), for example "api.example.com/v1/users/*". An empty
// method matches every method.
type Route struct {
	method, pattern string
	outcomes        []Outcome
	next            int
}

// On declares a fake route with a method, pattern, and outcome sequence.
func On(method, pattern string, outcomes ...Outcome) Route {
	return Route{method: method, pattern: pattern, outcomes: outcomes}
}

// Format writes a fixed label without exposing route data.
func (Route) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("HTTP fake route")) }

// Fake is a mutex-protected HTTP transport fake with queued outcomes and bounded request
// recording.
type Fake struct {
	mu       sync.Mutex
	outcomes []Outcome
	next     int
	routes   []Route
	requests []Request
	bytes    int
}

// New answers requests with outcomes in arrival order.
func New(outcomes ...Outcome) (*Fake, error) {
	owned, err := ownOutcomes(outcomes, 0)
	if err != nil {
		return nil, err
	}
	return &Fake{outcomes: owned}, nil
}

// NewRoutes answers each request from the first matching Route. A request
// matching no route fails with Unmatched; an exhausted route with Exhausted.
func NewRoutes(routes ...Route) (*Fake, error) {
	if len(routes) == 0 || len(routes) > 256 {
		return nil, invalid()
	}
	fake := &Fake{routes: make([]Route, len(routes))}
	total := 0
	for i, route := range routes {
		if len(route.method) > 32 || route.pattern == "" || len(route.pattern) > MaxRecordedURLBytes {
			return nil, invalid()
		}
		if _, err := path.Match(route.pattern, ""); err != nil {
			return nil, invalid()
		}
		owned, err := ownOutcomes(route.outcomes, total)
		if err != nil {
			return nil, err
		}
		for _, outcome := range owned {
			total += len(outcome.body) + headerBytes(outcome.headers)
		}
		fake.routes[i] = Route{method: route.method, pattern: route.pattern, outcomes: owned}
	}
	return fake, nil
}
func ownOutcomes(outcomes []Outcome, total int) ([]Outcome, error) {
	if len(outcomes) > MaxRequests {
		return nil, invalid()
	}
	owned := make([]Outcome, len(outcomes))
	for i, outcome := range outcomes {
		if outcome.failure == nil && (outcome.status < 100 || outcome.status > 599) || len(outcome.body) > MaxRecordedBodyBytes || len(outcome.headers) > 128 {
			return nil, invalid()
		}
		size := headerBytes(outcome.headers)
		if size < 0 {
			return nil, invalid()
		}
		total += len(outcome.body) + size
		if total > MaxRecordedBytes {
			return nil, invalid()
		}
		owned[i] = Outcome{status: outcome.status, headers: outcome.headers.Clone(), body: slices.Clone(outcome.body), failure: outcome.failure}
	}
	return owned, nil
}

// take selects the next outcome for a request; callers hold f.mu.
func (f *Fake) take(method string, address *url.URL) (Outcome, error) {
	if f.routes == nil {
		if f.next >= len(f.outcomes) {
			return Outcome{}, Exhausted
		}
		f.next++
		return f.outcomes[f.next-1], nil
	}
	target := address.Host + address.EscapedPath()
	for i := range f.routes {
		route := &f.routes[i]
		if matched, _ := path.Match(route.pattern, target); !matched || route.method != "" && route.method != method {
			continue
		}
		if route.next >= len(route.outcomes) {
			return Outcome{}, Exhausted
		}
		route.next++
		return route.outcomes[route.next-1], nil
	}
	return Outcome{}, Unmatched
}

func headerBytes(headers http.Header) int {
	if len(headers) > 128 {
		return -1
	}
	total, count := 0, 0
	for name, values := range headers {
		count += len(values)
		total += len(name)
		if count > 256 {
			return -1
		}
		for _, value := range values {
			if len(value) > (64<<10)-total {
				return -1
			}
			total += len(value)
		}
		if total > 64<<10 {
			return -1
		}
	}
	return total
}

// RoundTrip records a bounded request and returns the next matching fake outcome. It consumes and
// closes the request body.
func (f *Fake) RoundTrip(request *http.Request) (response *http.Response, err error) {
	if request == nil {
		return nil, invalid()
	}
	if request.Body != nil {
		defer func() {
			if closeErr := request.Body.Close(); closeErr != nil {
				response = nil
				err = closeErr
			}
		}()
	}
	if f == nil || request.URL == nil {
		return nil, invalid()
	}
	// Reject oversized metadata before cloning it, including when the fake is
	// used directly as a standard RoundTripper rather than through Client.
	headerSize := headerBytes(request.Header)
	u := request.URL
	if len(request.Method) > 32 || headerSize < 0 || u.User != nil || len(u.Scheme)+len(u.Opaque)+len(u.Host)+len(u.Path)+len(u.RawPath)+len(u.RawQuery)+len(u.Fragment)+len(u.RawFragment) > MaxRecordedURLBytes {
		return nil, invalid()
	}
	address := u.String()
	if len(address) > MaxRecordedURLBytes {
		return nil, invalid()
	}
	// Read the complete body before recording, so Requests never observes a
	// placeholder for a request whose body is still being consumed.
	var data []byte
	if request.Body != nil {
		data, err = io.ReadAll(io.LimitReader(request.Body, MaxRecordedBodyBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > MaxRecordedBodyBytes {
			return nil, invalid()
		}
	}
	size := len(request.Method) + len(address) + len(data) + headerSize
	f.mu.Lock()
	if len(f.requests) >= MaxRequests || size > MaxRecordedBytes-f.bytes {
		f.mu.Unlock()
		return nil, invalid()
	}
	f.bytes += size
	f.requests = append(f.requests, Request{method: request.Method, url: address, headers: request.Header.Clone(), body: data})
	if err := request.Context().Err(); err != nil {
		f.mu.Unlock()
		return nil, err
	}
	outcome, selectErr := f.take(request.Method, u)
	f.mu.Unlock()
	if selectErr != nil {
		return nil, selectErr
	}
	if outcome.failure != nil {
		return nil, outcome.failure
	}
	return &http.Response{StatusCode: outcome.status, Header: outcome.headers.Clone(), Body: io.NopCloser(bytes.NewReader(outcome.body)), ContentLength: int64(len(outcome.body)), Request: request}, nil
}

// Requests returns a synchronized snapshot with independent header maps and body slices.
func (f *Fake) Requests() []Request {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	result := slices.Clone(f.requests)
	for i := range result {
		result[i].headers = result[i].headers.Clone()
		result[i].body = slices.Clone(result[i].body)
	}
	return result
}

// Pending returns the synchronized count of unused global and route outcomes.
func (f *Fake) Pending() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pending := len(f.outcomes) - f.next
	for _, route := range f.routes {
		pending += len(route.outcomes) - route.next
	}
	return pending
}

// AssertSent fails t unless a recorded request satisfies match.
func (f *Fake) AssertSent(t testing.TB, match func(Request) bool) {
	t.Helper()
	if f.count(match) == 0 {
		t.Fatal("expected outbound HTTP request was not sent")
	}
}

// AssertNotSent fails t when any recorded request satisfies match.
func (f *Fake) AssertNotSent(t testing.TB, match func(Request) bool) {
	t.Helper()
	if f.count(match) != 0 {
		t.Fatal("unexpected outbound HTTP request was sent")
	}
}

// AssertSentCount fails t unless exactly expected requests satisfy match.
func (f *Fake) AssertSentCount(t testing.TB, expected int, match func(Request) bool) {
	t.Helper()
	if actual := f.count(match); actual != expected {
		t.Fatalf("outbound HTTP request count %d, expected %d", actual, expected)
	}
}
func (f *Fake) count(match func(Request) bool) int {
	if match == nil {
		match = func(Request) bool { return true }
	}
	count := 0
	for _, request := range f.Requests() {
		if match(request) {
			count++
		}
	}
	return count
}

// Sent returns the synchronized recorded request count; a nil fake returns zero.
func (f *Fake) Sent() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// Format writes a fixed label without exposing fake requests.
func (*Fake) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("outbound HTTP fake")) }
func invalid() error {
	return fault.New(fault.Invalid, "invalid or exhausted HTTP fake resource bound")
}
