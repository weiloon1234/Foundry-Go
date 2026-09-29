package http

import (
	"bufio"
	"io"
	"net"
	stdhttp "net/http"
)

// observedResponse records transport metadata without buffering or retaining
// response bytes. Native controls retain the same ownership/capability adapter
// used by compression and conditional responses.
type observedResponse struct {
	native   stdhttp.ResponseWriter
	status   int
	bytes    int64
	hijacked bool
	err      error
}

func (w *observedResponse) Header() stdhttp.Header                   { return w.native.Header() }
func (w *observedResponse) underlyingWriter() stdhttp.ResponseWriter { return w.native }
func (w *observedResponse) Unwrap() stdhttp.ResponseWriter           { return responseController{w} }

func (w *observedResponse) WriteHeader(status int) {
	w.native.WriteHeader(status)
	if !w.hijacked && w.status == 0 && (status >= 200 || status == stdhttp.StatusSwitchingProtocols) {
		w.status = status
	}
}

func (w *observedResponse) Write(data []byte) (int, error) {
	if !w.hijacked && w.status == 0 {
		w.status = stdhttp.StatusOK
	}
	n, err := w.native.Write(data)
	w.bytes += int64(n)
	if err != nil {
		w.err = err
	}
	return n, err
}

// ReadFrom delegates to the native writer's ReaderFrom when it has one, so a
// local file can reach the kernel's sendfile path; the byte count is recorded
// from its result. Otherwise Write is used, hiding ReaderFrom to avoid recursion.
func (w *observedResponse) ReadFrom(source io.Reader) (int64, error) {
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
	if err != nil {
		w.err = err
	}
	return n, err
}

func (w *observedResponse) FlushError() error {
	if !responseFlushSupported(w.native) {
		return stdhttp.ErrNotSupported
	}
	err := stdhttp.NewResponseController(w.native).Flush()
	if err != nil {
		w.err = err
	} else if w.status == 0 {
		w.status = stdhttp.StatusOK
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
