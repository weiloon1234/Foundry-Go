package http

import (
	"bufio"
	"net"
	stdhttp "net/http"
)

// Shared response capabilities keep native controls inside the body owner.
func (w *compressionResponse) FlushError() error                        { return w.flush() }
func (w *compressionResponse) underlyingWriter() stdhttp.ResponseWriter { return w.underlying }
func (w *compressionResponse) enableFullDuplexResponse() error {
	if w.hijacked {
		return stdhttp.ErrHijacked
	}
	if w.err != nil {
		return w.err
	}
	if err := w.request.Context().Err(); err != nil {
		return err
	}
	if w.mode == compressionEncoded {
		return stdhttp.ErrNotSupported
	}
	if err := stdhttp.NewResponseController(w.underlying).EnableFullDuplex(); err != nil {
		return err
	}
	w.passthrough = true
	if w.status != 0 {
		w.commit(false)
	}
	return w.err
}
func (w *compressionResponse) hijackResponse() (net.Conn, *bufio.ReadWriter, error) {
	if w.hijacked {
		return nil, nil, stdhttp.ErrHijacked
	}
	if w.err != nil {
		return nil, nil, w.err
	}
	if err := w.request.Context().Err(); err != nil {
		return nil, nil, err
	}
	if w.mode == compressionEncoded || len(w.buffer) != 0 || w.status != 0 && w.mode == compressionPending {
		return nil, nil, stdhttp.ErrNotSupported
	}
	conn, rw, err := stdhttp.NewResponseController(w.underlying).Hijack()
	if err == nil {
		w.hijacked = true
		w.release()
	}
	return conn, rw, err
}
