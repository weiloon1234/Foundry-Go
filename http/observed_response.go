package http

import (
	"bufio"
	"io"
	"net"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
)

// observedResponse records transport metadata and an optional bounded capture
// head. Native controls retain the same ownership/capability adapter
// used by compression and conditional responses.
type observedResponse struct {
	native           stdhttp.ResponseWriter
	protectedHeaders stdhttp.Header
	status           int
	bytes            int64
	hijacked         bool
	err              error
	head             *captureResponse
}

// Header returns the native writer's mutable response headers.
func (w *observedResponse) Header() stdhttp.Header                   { return w.native.Header() }
func (w *observedResponse) underlyingWriter() stdhttp.ResponseWriter { return w.native }

// Unwrap exposes the wrapper's supported response-control operations.
func (w *observedResponse) Unwrap() stdhttp.ResponseWriter { return responseController{w} }

// WriteHeader forwards status and records the first final status unless the response
// was hijacked.
func (w *observedResponse) WriteHeader(status int) {
	w.protectHeaders()
	w.native.WriteHeader(status)
	if !w.hijacked && w.status == 0 && (status >= 200 || status == stdhttp.StatusSwitchingProtocols) {
		w.status = status
		w.head.commit(w.Header())
	}
}

// Write forwards body data and records the status, written byte count and error.
func (w *observedResponse) Write(data []byte) (int, error) {
	w.protectHeaders()
	if !w.hijacked && w.status == 0 {
		w.status = stdhttp.StatusOK
	}
	n, err := w.native.Write(data)
	w.bytes += int64(n)
	if !w.hijacked {
		w.head.commit(w.Header())
		w.head.wrote(data, n)
	}
	if err != nil {
		w.err = err
	}
	return n, err
}

// ReadFrom delegates to the native writer's ReaderFrom when it has one, so a
// local file can reach the kernel's sendfile path; the byte count is recorded
// from its result. Otherwise Write is used, hiding ReaderFrom to avoid recursion.
func (w *observedResponse) ReadFrom(source io.Reader) (int64, error) {
	var prefixBytes int64
	if w.head != nil && !w.head.suppressBody && !w.hijacked {
		remaining := httpcapture.ResponseBytes - len(w.head.prefix)
		if remaining > 0 {
			n, err := io.CopyN(struct{ io.Writer }{w}, source, int64(remaining))
			prefixBytes = n
			if err != nil {
				if err == io.EOF {
					err = nil
				} else {
					w.err = err
				}
				return n, err
			}
		}
	}
	w.protectHeaders()
	if !w.hijacked && w.status == 0 {
		w.status = stdhttp.StatusOK
	}
	var n int64
	var err error
	if native, ok := w.native.(io.ReaderFrom); ok {
		n, err = native.ReadFrom(source)
	} else {
		n, err = io.Copy(struct{ io.Writer }{w.native}, source)
	}
	w.bytes += n
	if !w.hijacked {
		w.head.commit(w.Header())
		if w.head != nil && !w.head.suppressBody && n > 0 {
			w.head.truncated = true
		}
	}
	if err != nil {
		w.err = err
	}
	return prefixBytes + n, err
}

// FlushError flushes the native writer and records the status or error, rejecting
// unsupported flushing.
func (w *observedResponse) FlushError() error {
	w.protectHeaders()
	if !responseFlushSupported(w.native) {
		return stdhttp.ErrNotSupported
	}
	err := stdhttp.NewResponseController(w.native).Flush()
	if err != nil {
		w.err = err
	} else if w.status == 0 {
		w.status = stdhttp.StatusOK
		w.head.commit(w.Header())
	}
	return err
}

func (w *observedResponse) hijackResponse() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := stdhttp.NewResponseController(w.native).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

func (w *observedResponse) enableFullDuplexResponse() error {
	return stdhttp.NewResponseController(w.native).EnableFullDuplex()
}

// protectHeaders pins framework-owned transport policy before the first commit.
// An empty slice removes a header, including an inapplicable credential challenge.
func (w *observedResponse) protectHeaders() {
	if w.status != 0 {
		return
	}
	for name, values := range w.protectedHeaders {
		w.Header().Del(name)
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
}
