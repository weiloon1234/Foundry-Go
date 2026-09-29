package http

import (
	"io"
	stdhttp "net/http"
	"slices"
	"time"
)

func (w *etagResponse) finish() error {
	if w.hijacked {
		return nil
	}
	if w.err != nil {
		return w.err
	}
	if w.finalized {
		return nil
	}
	if err := transportErr(w.request); err != nil {
		return err
	}
	if w.status == 0 {
		w.WriteHeader(stdhttp.StatusOK)
	}
	if w.err != nil {
		return w.err
	}
	if w.committed {
		publishResponseTrailers(w.native.Header(), w.final, w.header)
		return nil
	}
	if hasResponseTrailers(w.header) {
		if err := w.commit(); err != nil {
			return err
		}
		publishResponseTrailers(w.native.Header(), w.final, w.header)
		return nil
	}
	if w.declared >= 0 && w.declared != w.buffer.size {
		return io.ErrUnexpectedEOF
	}
	return w.serveValidated(w.buffer.reader(), w.buffer.entityTag(), w.buffer.size)
}

// serveValidated publishes one complete representation with its strong
// validator. ServeContent owns weak/list/wildcard conditions and their precedence.
// Range-bearing requests bypass capture before the handler runs; retaining the
// handler's original Accept-Ranges avoids advertising invented support.
func (w *etagResponse) serveValidated(content io.ReadSeeker, tag EntityTag, size int64) error {
	writer := newFileResponseWriter(transportParent(w.request), w.native)
	copyResponseHeaders(writer.Header(), w.final)
	// net/http does not infer a media type when a handler writes no body.
	// An explicit nil entry prevents ServeContent from sniffing empty bytes.
	if _, explicit := writer.Header()["Content-Type"]; !explicit && size == 0 {
		writer.Header()["Content-Type"] = nil
	}
	writer.Header().Set("ETag", string(tag))
	ranges, hadRanges := w.final["Accept-Ranges"]
	conditional := etagConditionWriter{fileResponseWriter: writer, ranges: slices.Clone(ranges), hadRanges: hadRanges, success: w.status}
	var modified time.Time
	if text := w.final.Get("Last-Modified"); text != "" {
		modified, _ = stdhttp.ParseTime(text)
	}
	stdhttp.ServeContent(conditional, w.request, "", modified, content)
	w.committed = writer.committed
	if writer.failed != nil {
		return writer.failed
	}
	if writer.status >= 400 {
		copyResponseHeaders(w.native.Header(), w.final)
		code := InternalError
		if writer.status == stdhttp.StatusPreconditionFailed {
			code = PreconditionFailed
		}
		writeRoutingError(w.native, w.request, code)
	}
	return nil
}

type etagConditionWriter struct {
	*fileResponseWriter
	ranges    []string
	hadRanges bool
	success   int
}

func (w etagConditionWriter) WriteHeader(status int) {
	if status == stdhttp.StatusOK {
		status = w.success
	}
	if w.hadRanges {
		w.Header()["Accept-Ranges"] = slices.Clone(w.ranges)
	} else {
		w.Header().Del("Accept-Ranges")
	}
	w.fileResponseWriter.WriteHeader(status)
}
