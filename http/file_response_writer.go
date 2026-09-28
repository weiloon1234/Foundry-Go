package http

import (
	"context"
	"io"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Native range/precondition failures are held before commit so the endpoint
// can use its shared JSON errors. Successful representations stream directly;
// no whole-body buffer or ReaderFrom fast path bypasses the write accounting.
type fileResponseWriter struct {
	ctx       context.Context
	native    stdhttp.ResponseWriter
	header    stdhttp.Header
	status    int
	committed bool
	written   int64
	failed    error
}

func newFileResponseWriter(ctx context.Context, w stdhttp.ResponseWriter) *fileResponseWriter {
	return &fileResponseWriter{ctx: ctx, native: w, header: w.Header().Clone()}
}
func (w *fileResponseWriter) Header() stdhttp.Header { return w.header }
func (w *fileResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if status >= 400 {
		return
	}
	if err := w.ctx.Err(); err != nil {
		w.failed = err
		return
	}
	// A failed writer may have partially committed; never attempt a replacement.
	w.committed = true
	copyResponseHeaders(w.native.Header(), w.header)
	w.native.WriteHeader(status)
}
func (w *fileResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(stdhttp.StatusOK)
	}
	if w.status >= 400 {
		return len(data), nil
	}
	if w.failed != nil {
		return 0, errFileTransfer
	}
	if err := w.ctx.Err(); err != nil {
		w.failed = err
		return 0, errFileTransfer
	}
	n, err := w.native.Write(data)
	if n < 0 || n > len(data) {
		w.failed = fault.New(fault.Internal, "HTTP writer returned an invalid count")
		return 0, errFileTransfer
	}
	w.written += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.failed = err
		return n, errFileTransfer
	}
	return n, nil
}

// WriteError owns all ordinary error headers. A native 416 additionally carries
// its computed Content-Range, injected only at the final error-header boundary.
type fileRangeErrorWriter struct {
	stdhttp.ResponseWriter
	contentRange string
}

func (w fileRangeErrorWriter) WriteHeader(status int) {
	if status == stdhttp.StatusRequestedRangeNotSatisfiable && w.contentRange != "" {
		w.Header().Set("Content-Range", w.contentRange)
	}
	w.ResponseWriter.WriteHeader(status)
}
