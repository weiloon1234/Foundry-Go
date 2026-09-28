package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
)

// Body is an immutable body source. Zero means no body. Stream openers transfer
// ownership of a new reader per call and must support concurrent Read/Close.
type Body struct {
	data   []byte
	open   func(context.Context) (io.ReadCloser, error)
	length int64
	replay bool
	err    error
}

func Bytes(data []byte) Body {
	if int64(len(data)) > MaxBodyBytes {
		return Body{err: invalid()}
	}
	return Body{data: slices.Clone(data), length: int64(len(data)), replay: true}
}
func StreamBody(length int64, open func(context.Context) (io.ReadCloser, error)) Body {
	return streamBody(length, open, false)
}

// ReplayableBody asserts each opener returns the same complete content in an
// independent reader. Retry safety still needs a matching operation policy.
func ReplayableBody(length int64, open func(context.Context) (io.ReadCloser, error)) Body {
	return streamBody(length, open, true)
}
func streamBody(length int64, open func(context.Context) (io.ReadCloser, error), replay bool) Body {
	if length < -1 || length > MaxBodyBytes || open == nil {
		return Body{err: invalid()}
	}
	return Body{open: open, length: length, replay: replay}
}
func (b Body) replayable() bool { return b.open == nil || b.replay }
func (b Body) reader(ctx context.Context) (io.ReadCloser, error) {
	if b.open != nil {
		return b.open(ctx)
	}
	if len(b.data) == 0 {
		return http.NoBody, nil
	}
	return io.NopCloser(bytes.NewReader(b.data)), nil
}
func (Body) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("HTTP body")) }
