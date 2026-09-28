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

func (r Response) Status() int          { return r.info.status }
func (r Response) Attempts() int        { return r.info.attempt }
func (r Response) Headers() http.Header { return r.info.headers.Clone() }
func (r Response) Bytes() []byte        { return slices.Clone(r.data) }
func (r Response) Text() (string, error) {
	if r.info.status == 0 || !utf8.Valid(r.data) {
		return "", failure(DecodeFailed, r.info.name, r.info.method, r.info.attempt, r.info.status, nil)
	}
	return string(r.data), nil
}
func (r Response) EnsureSuccess() error { return r.info.ensureSuccess() }
func (r Response) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprintf(state, "HTTP response (status %d, bytes %d)", r.info.status, len(r.data))
}

type StreamResponse struct {
	info responseInfo
	body *ownedstream.Body
}

func (r *StreamResponse) Status() int {
	if r == nil {
		return 0
	}
	return r.info.status
}
func (r *StreamResponse) Attempts() int {
	if r == nil {
		return 0
	}
	return r.info.attempt
}
func (r *StreamResponse) Headers() http.Header {
	if r == nil {
		return nil
	}
	return r.info.headers.Clone()
}
func (r *StreamResponse) Read(p []byte) (int, error) {
	if r == nil {
		return 0, invalid()
	}
	return r.body.Read(p)
}
func (r *StreamResponse) EnsureSuccess() error {
	if r == nil {
		return invalid()
	}
	return r.info.ensureSuccess()
}
func (r *StreamResponse) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprintf(state, "HTTP stream (status %d)", r.Status())
}
