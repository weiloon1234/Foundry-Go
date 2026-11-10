package http

import (
	"context"
	"io"
	"net"
	stdhttp "net/http"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/value"
)

// replayCaptureBody owns the kernel-limited source. The prefix is immutable
// after preread; source observations count bytes only once, including later
// handler reads. Close may run concurrently with a blocked Read, as net/http
// requires. Concurrent Reads are not supported by net/http's Body contract.
type replayCaptureBody struct {
	source    io.ReadCloser
	prefix    []byte
	prefixMu  sync.Mutex
	position  int
	terminal  error
	observed  atomic.Int64
	eof       atomic.Bool
	failure   atomic.Pointer[captureReadFailure]
	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool
}

type captureReadFailure struct{ code httpcapture.ReadFailure }

func (c *kernelCapture) preread(body io.ReadCloser, declared int64) io.ReadCloser {
	if body == nil {
		return nil
	}
	// Reserve the whole allowance before entering here, but avoid retaining a
	// full capture-cap allocation for ordinary small requests. Grow only once;
	// the reservation's metadata allowance covers this initial buffer too.
	size := min(bodyPresize, c.limit+1)
	if declared >= 0 && declared < int64(size) {
		size = int(declared) + 1
	}
	b := &replayCaptureBody{source: body, prefix: make([]byte, size)}
	c.replay = b
	used := 0
	for used < len(b.prefix) {
		n, err := b.readSource(b.prefix[used:])
		used += n
		if err != nil {
			break
		}
		if used == len(b.prefix) && used < c.limit+1 {
			prefix := make([]byte, c.limit+1)
			copy(prefix, b.prefix)
			b.prefix = prefix
		}
	}
	b.prefix = b.prefix[:used]
	return b
}

func (b *replayCaptureBody) readSource(p []byte) (int, error) {
	if b.terminal != nil {
		return 0, b.terminal
	}
	n, err := b.source.Read(p)
	b.observed.Add(int64(n))
	if err != nil {
		b.terminal = err
		if err == io.EOF {
			b.eof.Store(true)
		} else {
			b.failure.Store(&captureReadFailure{captureFailureCode(err)})
		}
	}
	return n, err
}

func captureFailureCode(err error) httpcapture.ReadFailure {
	if _, ok := err.(*stdhttp.MaxBytesError); ok {
		return httpcapture.BodyLimit
	}
	if err == io.ErrUnexpectedEOF || err == context.Canceled {
		return httpcapture.ClientDisconnect
	}
	code := httpcapture.ReadFailed
	// Timeout is an extension method too. Never format an I/O error or let its
	// methods escape the existing callback isolation owner.
	_ = callback.Isolated("HTTP capture read classification", func() error {
		if timed, ok := err.(net.Error); ok && timed.Timeout() {
			code = httpcapture.ReadTimeout
		}
		return nil
	})
	return code
}

// Read returns the preread prefix, then the untouched remainder of the source.
func (b *replayCaptureBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.prefixMu.Lock()
	if b.closed.Load() {
		b.prefixMu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if b.position < len(b.prefix) {
		n := copy(p, b.prefix[b.position:])
		b.position += n
		b.prefixMu.Unlock()
		return n, nil
	}
	b.prefixMu.Unlock()
	return b.readSource(p)
}

// Close closes the original source once; later reads report a closed body.
func (b *replayCaptureBody) Close() error {
	b.closeOnce.Do(func() { b.closed.Store(true); b.closeErr = b.source.Close() })
	return b.closeErr
}

func (b *replayCaptureBody) snapshot(limit int) httpcapture.Body {
	b.prefixMu.Lock()
	defer b.prefixMu.Unlock()
	s := httpcapture.Body{Prefix: b.prefix, ObservedBytes: b.observed.Load(), End: httpcapture.OverCaptureLimit}
	b.prefix = nil // transfer ownership; a closed reader cannot replay it again
	if b.eof.Load() {
		s.MeasuredBytes = value.Of(s.ObservedBytes)
		if s.ObservedBytes <= int64(limit) {
			s.End = httpcapture.EOF
		}
	}
	if failure := b.failure.Load(); failure != nil {
		s.End, s.ReadFailure = httpcapture.SourceFailure, failure.code
	}
	return s
}

type captureResponse struct {
	prefix                []byte
	suppressBody          bool
	truncated             bool
	committed             bool
	contentType, encoding string
	headers               []httpcapture.Field
	metadataTruncated     bool
}

func (c *captureResponse) wrote(data []byte, n int) {
	if c == nil || c.suppressBody {
		return
	}
	remaining := httpcapture.ResponseBytes - len(c.prefix)
	c.prefix = append(c.prefix, data[:min(n, remaining)]...)
	c.truncated = c.truncated || n > remaining
}

func (c *captureResponse) commit(header stdhttp.Header) {
	if c == nil || c.committed {
		return
	}
	c.committed = true
	var typeCut, encodingCut, headersCut bool
	c.contentType, typeCut = captureString(header.Get("Content-Type"), 256)
	c.encoding, encodingCut = captureString(header.Get("Content-Encoding"), 256)
	c.headers, headersCut = captureHeaders(header)
	c.metadataTruncated = typeCut || encodingCut || headersCut
}
