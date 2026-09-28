// Package httpclient supplies deterministic outbound HTTP transports for tests.
// A fake never falls through to the network. Recorded data is explicitly private
// test data; ordinary formatting omits URLs, headers and bodies.
package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const MaxRequests = 1024
const MaxRecordedBodyBytes = 1 << 20
const MaxRecordedBytes = 16 << 20
const MaxRecordedURLBytes = 16 << 10

var Exhausted = fault.New(fault.Missing, "outbound HTTP fake has no remaining outcome")

type Outcome struct {
	status  int
	headers http.Header
	body    []byte
	failure error
}

func Respond(status int, headers http.Header, body []byte) Outcome {
	if len(body) > MaxRecordedBodyBytes || headerBytes(headers) < 0 {
		return Outcome{}
	}
	return Outcome{status: status, headers: headers.Clone(), body: slices.Clone(body)}
}
func Fail(err error) Outcome                   { return Outcome{failure: err} }
func (Outcome) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("HTTP fake outcome")) }

type Request struct {
	method, url string
	headers     http.Header
	body        []byte
}

func (r Request) Method() string               { return r.method }
func (r Request) URL() string                  { return r.url }
func (r Request) Headers() http.Header         { return r.headers.Clone() }
func (r Request) Body() []byte                 { return slices.Clone(r.body) }
func (Request) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("recorded HTTP request")) }

type Fake struct {
	mu       sync.Mutex
	outcomes []Outcome
	next     int
	requests []Request
	bytes    int
}

func New(outcomes ...Outcome) (*Fake, error) {
	if len(outcomes) > MaxRequests {
		return nil, invalid()
	}
	owned := make([]Outcome, len(outcomes))
	total := 0
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
	return &Fake{outcomes: owned}, nil
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
	f.mu.Lock()
	if len(f.requests) >= MaxRequests {
		f.mu.Unlock()
		return nil, invalid()
	}
	index := len(f.requests)
	f.requests = append(f.requests, Request{})
	var outcome Outcome
	available := f.next < len(f.outcomes)
	if available {
		outcome = f.outcomes[f.next]
		f.next++
	}
	f.mu.Unlock()
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
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	size := len(request.Method) + len(address) + len(data) + headerSize
	f.mu.Lock()
	if size > MaxRecordedBytes-f.bytes {
		f.mu.Unlock()
		return nil, invalid()
	}
	f.bytes += size
	f.requests[index] = Request{method: request.Method, url: address, headers: request.Header.Clone(), body: slices.Clone(data)}
	f.mu.Unlock()
	if !available {
		return nil, Exhausted
	}
	if outcome.failure != nil {
		return nil, outcome.failure
	}
	return &http.Response{StatusCode: outcome.status, Header: outcome.headers.Clone(), Body: io.NopCloser(bytes.NewReader(outcome.body)), ContentLength: int64(len(outcome.body)), Request: request}, nil
}
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
func (f *Fake) Pending() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.outcomes) - f.next
}
func (f *Fake) Sent() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}
func (*Fake) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("outbound HTTP fake")) }
func invalid() error {
	return fault.New(fault.Invalid, "invalid or exhausted HTTP fake resource bound")
}
