package httpclient

import (
	"fmt"
	"net/http"
	"slices"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/ownedstream"
)

type responseInfo struct {
	name            Name
	method          string
	status, attempt int
	headers         http.Header
	maximum         int64
}

func (i responseInfo) ensureSuccess() error {
	if i.status < 200 || i.status >= 300 {
		return failure(StatusFailed, i.name, i.method, i.attempt, i.status, nil)
	}
	return nil
}

// Response owns a complete, bounded body and copied response headers. Accessors
// exposing bytes/headers are explicit and return independent snapshots.
type Response struct {
	info responseInfo
	data []byte
}

// Status returns the HTTP status code.
func (r Response) Status() int { return r.info.status }

// Attempts returns the number of the final request attempt.
func (r Response) Attempts() int { return r.info.attempt }

// Headers returns a copy of the response headers.
func (r Response) Headers() http.Header { return r.info.headers.Clone() }

// Bytes returns a copy of the buffered body.
func (r Response) Bytes() []byte { return slices.Clone(r.data) }

// Text returns the body as UTF-8 text, or a DecodeFailed error for invalid UTF-8 or an
// uninitialized response.
func (r Response) Text() (string, error) {
	if r.info.status == 0 || !utf8.Valid(r.data) {
		return "", failure(DecodeFailed, r.info.name, r.info.method, r.info.attempt, r.info.status, nil)
	}
	return string(r.data), nil
}

// EnsureSuccess returns a StatusFailed error unless the status is in the 2xx range.
func (r Response) EnsureSuccess() error { return r.info.ensureSuccess() }

// Format writes the response status and body length without exposing content.
func (r Response) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprintf(state, "HTTP response (status %d, bytes %d)", r.info.status, len(r.data))
}

// StreamResponse exposes the response body within the client's streaming callback
// lifetime.
type StreamResponse struct {
	info responseInfo
	body *ownedstream.Body
}

// Status returns the HTTP status code, or zero for a nil response.
func (r *StreamResponse) Status() int {
	if r == nil {
		return 0
	}
	return r.info.status
}

// Attempts returns the final attempt number, or zero for a nil response.
func (r *StreamResponse) Attempts() int {
	if r == nil {
		return 0
	}
	return r.info.attempt
}

// Headers returns a copy of the headers, or nil for a nil response.
func (r *StreamResponse) Headers() http.Header {
	if r == nil {
		return nil
	}
	return r.info.headers.Clone()
}

// Read reads from the owned response body; a nil response is invalid.
func (r *StreamResponse) Read(p []byte) (int, error) {
	if r == nil {
		return 0, invalid()
	}
	return r.body.Read(p)
}

// EnsureSuccess checks for a 2xx status; a nil response is invalid.
func (r *StreamResponse) EnsureSuccess() error {
	if r == nil {
		return invalid()
	}
	return r.info.ensureSuccess()
}

// Format writes the response status without exposing content.
func (r *StreamResponse) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprintf(state, "HTTP stream (status %d)", r.Status())
}
