package http

import (
	"bufio"
	"io"
	"net"
	stdhttp "net/http"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type etagResponse struct {
	native                        stdhttp.ResponseWriter
	request                       *stdhttp.Request
	header, final                 stdhttp.Header
	status                        int
	declared                      int64
	buffer                        responseBuffer
	permits                       chan struct{}
	acquired, committed, hijacked bool
	err                           error
	passthrough                   bool
}

func (w *etagResponse) Header() stdhttp.Header                   { return w.header }
func (w *etagResponse) underlyingWriter() stdhttp.ResponseWriter { return w.native }
func (w *etagResponse) Unwrap() stdhttp.ResponseWriter           { return responseController{w} }

func (w *etagResponse) WriteHeader(status int) {
	if status < 100 || status > 999 {
		panic("invalid HTTP status code")
	}
	if w.status != 0 || w.hijacked {
		return
	}
	if status < 200 && status != stdhttp.StatusSwitchingProtocols {
		copyResponseHeaders(w.native.Header(), w.header)
		w.native.WriteHeader(status)
		return
	}
	w.status = status
	w.final = w.header.Clone()
	if w.passthrough || !automaticETagAllowed(status, w.final) {
		_ = w.commit()
		return
	}
	if values := w.final.Values("Content-Length"); len(values) != 0 {
		if len(values) != 1 {
			_ = w.commit()
			return
		}
		length, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || length < 0 {
			_ = w.commit()
			return
		}
		w.declared = length
		if length > w.buffer.limit {
			_ = w.commit()
		}
	}
}

func (w *etagResponse) Write(data []byte) (int, error) {
	if w.hijacked {
		return 0, stdhttp.ErrHijacked
	}
	if w.err != nil {
		return 0, w.err
	}
	if err := w.request.Context().Err(); err != nil {
		w.err = err
		return 0, err
	}
	if w.status == 0 {
		w.WriteHeader(stdhttp.StatusOK)
	}
	if w.err != nil {
		return 0, w.err
	}
	if w.committed {
		return w.writeThrough(data)
	}
	if w.declared >= 0 && int64(len(data)) > w.declared-w.buffer.size {
		w.err = stdhttp.ErrContentLength
		return 0, w.err
	}
	if len(data) != 0 {
		if _, explicit := w.final["Content-Type"]; !explicit && w.final.Get("Content-Encoding") == "" {
			w.final.Set("Content-Type", stdhttp.DetectContentType(data[:min(len(data), 512)]))
		}
	}
	if w.buffer.append(data) {
		return len(data), nil
	}
	if err := w.commit(); err != nil {
		return 0, err
	}
	return w.writeThrough(data)
}

// commit abandons automatic hashing and transfers the complete accepted prefix.
// A failing prefix never causes later data to be sent out of order.
func (w *etagResponse) commit() error {
	if w.err != nil {
		return w.err
	}
	if w.committed || w.hijacked {
		return nil
	}
	if w.status == 0 {
		w.WriteHeader(stdhttp.StatusOK)
	}
	if w.committed {
		return w.err
	}
	if err := w.request.Context().Err(); err != nil {
		w.err = err
		return err
	}
	w.committed = true
	copyResponseHeaders(w.native.Header(), w.final)
	w.native.WriteHeader(w.status)
	for _, page := range w.buffer.pages {
		if _, err := w.writeThrough(page); err != nil {
			break
		}
	}
	w.release()
	return w.err
}

func (w *etagResponse) writeThrough(data []byte) (int, error) {
	if len(data) == 0 {
		n, err := w.native.Write(data)
		if n != 0 {
			err = fault.New(fault.Internal, "HTTP response writer returned an invalid count")
		}
		w.err = err
		return 0, err
	}
	total := 0
	for len(data) != 0 {
		if err := w.request.Context().Err(); err != nil {
			w.err = err
			return total, err
		}
		chunk := data[:min(len(data), responseBufferPageBytes)]
		n, err := w.native.Write(chunk)
		if n < 0 || n > len(chunk) {
			w.err = fault.New(fault.Internal, "HTTP response writer returned an invalid count")
			return total, w.err
		}
		total += n
		if err == nil && n != len(chunk) {
			err = io.ErrShortWrite
		}
		if err != nil {
			w.err = err
			return total, err
		}
		data = data[n:]
	}
	return total, nil
}

func (w *etagResponse) release() {
	w.buffer.reset()
	if w.acquired {
		<-w.permits
		w.acquired = false
	}
}

func (w *etagResponse) FlushError() error {
	if w.hijacked {
		return stdhttp.ErrHijacked
	}
	if w.err != nil {
		return w.err
	}
	if !responseFlushSupported(w.native) {
		return stdhttp.ErrNotSupported
	}
	if err := w.commit(); err != nil {
		return err
	}
	w.err = stdhttp.NewResponseController(w.native).Flush()
	return w.err
}

func (w *etagResponse) hijackResponse() (net.Conn, *bufio.ReadWriter, error) {
	if w.hijacked {
		return nil, nil, stdhttp.ErrHijacked
	}
	if w.err != nil {
		return nil, nil, w.err
	}
	if err := w.request.Context().Err(); err != nil {
		return nil, nil, err
	}
	if w.buffer.size != 0 || w.status != 0 && !w.committed {
		return nil, nil, stdhttp.ErrNotSupported
	}
	conn, rw, err := stdhttp.NewResponseController(w.native).Hijack()
	if err == nil {
		w.hijacked = true
		w.release()
	}
	return conn, rw, err
}

// ReadFrom retains observed source failures, including a truncated io.Copy.
// The source owns cancellation of blocking reads; no detached reader is started.
func (w *etagResponse) ReadFrom(reader io.Reader) (int64, error) {
	if w.hijacked {
		return 0, stdhttp.ErrHijacked
	}
	if w.err != nil {
		return 0, w.err
	}
	data := make([]byte, responseBufferPageBytes)
	var total int64
	empty := 0
	for {
		if err := w.request.Context().Err(); err != nil {
			w.err = err
			return total, err
		}
		n, err := reader.Read(data)
		if n < 0 || n > len(data) {
			w.err = fault.New(fault.Internal, "HTTP response source returned an invalid count")
			return total, w.err
		}
		if n != 0 {
			empty = 0
			written, writeErr := w.Write(data[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			w.err = err
			return total, err
		}
		if n == 0 {
			empty++
			if empty >= 100 {
				w.err = io.ErrNoProgress
				return total, w.err
			}
		}
	}
}

// A native full-duplex handler can exchange data before returning. Once enabled,
// retain native streaming thresholds instead of waiting for complete-body hashing.
func (w *etagResponse) enableFullDuplexResponse() error {
	if w.hijacked {
		return stdhttp.ErrHijacked
	}
	if w.err != nil {
		return w.err
	}
	if err := w.request.Context().Err(); err != nil {
		return err
	}
	if err := stdhttp.NewResponseController(w.native).EnableFullDuplex(); err != nil {
		return err
	}
	w.passthrough = true
	if w.status != 0 {
		return w.commit()
	}
	w.release()
	return nil
}
