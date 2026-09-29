package http

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Native ServeContent can expose a Seek error in its public response. Retain
// original causes on the owner and expose only a fixed, value-free I/O error.
var errFileTransfer = errors.New("file response could not be transferred")

// fileReader owns access to one source, including calls from ServeContent's
// multipart-range goroutine. Every custom operation waits for its real exit.
// Closing waits for active I/O, prevents later source access and runs exactly
// once, even on cancellation. It deliberately exposes no WriterTo fast path;
// only fileResponseWriter may transfer a framework-opened local file natively.
type fileReader struct {
	ctx        context.Context
	body       io.ReadCloser
	seeker     io.Seeker
	mu         sync.Mutex
	failed     error
	closed     bool
	closeErr   error
	emptyReads int
	maxBytes   int64
}

func newFileReader(ctx context.Context, body io.ReadCloser) (*fileReader, error) {
	if ctx == nil || body == nil {
		return nil, fault.New(fault.Invalid, "file response requires a context and body")
	}
	seeker, _ := body.(io.Seeker)
	return &fileReader{ctx: ctx, body: body, seeker: seeker}, nil
}

func (r *fileReader) ready() error {
	if r.closed {
		return errFileTransfer
	}
	if r.failed != nil {
		return errFileTransfer
	}
	if err := r.ctx.Err(); err != nil {
		return r.fail(err)
	}
	return nil
}

// fail is called only with mu held. Retain the first cause so a later native
// cleanup or canceled read cannot replace the failure that ended the transfer.
func (r *fileReader) fail(cause error) error {
	if r.failed == nil {
		r.failed = cause
	}
	return errFileTransfer
}

func (r *fileReader) Read(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ready(); err != nil {
		return 0, err
	}
	var n int
	var returned error
	// Reads stay Isolated: ServeContent's multipart-range producer calls this
	// from its own goroutine, and a Goexit there would leave its pipe open and
	// hang the response forever. Local files bypass per-chunk reads (sendTo).
	if err := callback.Isolated("HTTP file source read", func() error { n, returned = r.body.Read(data); return nil }); err != nil {
		return 0, r.fail(err)
	}
	if n < 0 || n > len(data) {
		return 0, r.fail(fault.New(fault.Internal, "file source returned an invalid read count"))
	}
	if returned != nil && returned != io.EOF {
		return n, r.fail(returned)
	}
	if err := r.ctx.Err(); err != nil {
		return n, r.fail(err)
	}
	if len(data) > 0 && n == 0 && returned == nil {
		r.emptyReads++
		if r.emptyReads >= 100 {
			return 0, r.fail(io.ErrNoProgress)
		}
	} else {
		r.emptyReads = 0
	}
	return n, returned
}

func (r *fileReader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ready(); err != nil {
		return 0, err
	}
	if r.seeker == nil {
		return 0, r.fail(fault.New(fault.Internal, "file response source is not seekable"))
	}
	var position int64
	var returned error
	if err := callback.Isolated("HTTP file source seek", func() error { position, returned = r.seeker.Seek(offset, whence); return nil }); err != nil {
		return 0, r.fail(err)
	}
	if returned != nil {
		return 0, r.fail(returned)
	}
	if position < 0 || whence == io.SeekStart && position != offset || r.maxBytes > 0 && position > r.maxBytes {
		return 0, r.fail(fault.New(fault.Internal, "file source returned an inconsistent position"))
	}
	if err := r.ctx.Err(); err != nil {
		return 0, r.fail(err)
	}
	return position, nil
}

func (r *fileReader) Failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed
}

func (r *fileReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	var returned error
	if err := callback.Isolated("HTTP file source close", func() error { returned = r.body.Close(); return nil }); err != nil {
		r.closeErr = err
	} else {
		r.closeErr = returned
	}
	return r.closeErr
}

// localFile reports the framework-opened regular file behind this source:
// LocalDownload bodies and directory-asset bodies. Application readers never
// qualify, so their reads keep the owned per-chunk boundary.
func (r *fileReader) localFile() *os.File {
	switch body := r.body.(type) {
	case *os.File:
		return body
	case *assetBody:
		file, _ := body.ReadSeekCloser.(*os.File)
		return file
	}
	return nil
}

// sendTo transfers up to limit bytes of a local file through the native
// writer's ReaderFrom while holding the source, so Close still waits for it.
// It reports handled=false without side effects when the source is not local.
func (r *fileReader) sendTo(native io.ReaderFrom, limit int64, w *fileResponseWriter) (int64, bool, error) {
	file := r.localFile()
	if file == nil {
		return 0, false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ready(); err != nil {
		return 0, true, err
	}
	if err := w.ctx.Err(); err != nil {
		w.failed = err
		return 0, true, errFileTransfer
	}
	n, err := native.ReadFrom(&io.LimitedReader{R: file, N: limit})
	if n < 0 || n > limit {
		w.failed = fault.New(fault.Internal, "HTTP writer returned an invalid count")
		return 0, true, errFileTransfer
	}
	w.written += n
	if err != nil {
		w.failed = err
		return n, true, errFileTransfer
	}
	return n, true, nil
}
