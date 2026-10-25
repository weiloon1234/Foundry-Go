package http

import (
	"io"
	stdhttp "net/http"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func (p *preparedFile) write(w stdhttp.ResponseWriter, r *stdhttp.Request) error {
	if p.stream {
		return p.writeStream(w, r)
	}
	var writer *fileResponseWriter
	owned := callback.Isolated("HTTP download response", func() error {
		writer = newFileResponseWriter(p.reader.ctx, w)
		header := writer.Header()
		p.headers(header)
		if p.tag != "" {
			header.Set("ETag", string(p.tag))
		}
		stdhttp.ServeContent(writer, r, "", p.modified, p.reader)
		return nil
	})
	return p.finish(w, r, writer, owned, nil)
}

func (p *preparedFile) finish(w stdhttp.ResponseWriter, r *stdhttp.Request, writer *fileResponseWriter, owned, transfer error) error {
	// Reader callbacks already have their own boundary. A failure here means
	// the native writer or transport itself failed; it may no longer be usable
	// even when Header panicked before commit. Never call that writer again to
	// attempt a replacement response.
	if owned != nil {
		return safeerror.WrapFailure(fault.Internal, "file response writer failed", owned)
	}
	cause := p.reader.Failure()
	if cause == nil && writer != nil {
		cause = writer.failed
	}
	if cause == nil {
		cause = transfer
	}
	if cause == nil && !p.stream && writer != nil && writer.committed && r.Method != stdhttp.MethodHead && (writer.status == 200 || writer.status == 206) {
		expected, err := strconv.ParseInt(writer.Header().Get("Content-Length"), 10, 64)
		if err != nil || writer.written != expected {
			cause = io.ErrUnexpectedEOF
		}
	}
	if cause != nil {
		if writer != nil && writer.committed {
			return safeerror.WrapFailure(fault.Internal, "file response transfer failed", cause)
		}
		writeRoutingError(w, r, fileReadError(r.Context(), cause))
		return nil
	}
	if writer == nil {
		return safeerror.NewFault(fault.Internal, "file response was not prepared")
	}
	if writer.status >= 400 {
		var responseError error = InternalError.WithCause(nil)
		switch writer.status {
		case stdhttp.StatusPreconditionFailed:
			responseError = PreconditionFailed.WithCause(nil)
		case stdhttp.StatusRequestedRangeNotSatisfiable:
			responseError = RangeNotSatisfiable.WithCause(nil)
		}
		writeRoutingError(fileRangeErrorWriter{ResponseWriter: w, contentRange: writer.Header().Get("Content-Range")}, r, responseError)
	}
	return nil
}
