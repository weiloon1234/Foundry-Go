package http

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type etagControlTransport struct {
	header                           stdhttp.Header
	body                             bytes.Buffer
	status, flushes, hijacks, pushes int
	deadline                         time.Time
}

func (w *etagControlTransport) Header() stdhttp.Header { return w.header }
func (w *etagControlTransport) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *etagControlTransport) Write(data []byte) (int, error) { return w.body.Write(data) }
func (w *etagControlTransport) Flush()                         { w.flushes++ }
func (w *etagControlTransport) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks++
	return nil, nil, nil
}
func (w *etagControlTransport) Push(string, *stdhttp.PushOptions) error { w.pushes++; return nil }
func (w *etagControlTransport) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

type etagOpaqueTransport struct{ stdhttp.ResponseWriter }
type etagTransparentTransport struct{ stdhttp.ResponseWriter }

func (w etagTransparentTransport) Unwrap() stdhttp.ResponseWriter { return w.ResponseWriter }

func TestResponseCapabilitiesPreserveNativeInterfacesAndOwnership(t *testing.T) {
	for _, mode := range []string{"direct", "transparent", "opaque"} {
		t.Run(mode, func(t *testing.T) {
			native := &etagControlTransport{header: make(stdhttp.Header)}
			var supplied stdhttp.ResponseWriter = native
			switch mode {
			case "transparent":
				supplied = etagTransparentTransport{native}
			case "opaque":
				supplied = etagOpaqueTransport{native}
			}
			deadline := time.Now().Add(time.Second)
			handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				_, flush := w.(stdhttp.Flusher)
				_, hijack := w.(stdhttp.Hijacker)
				_, push := w.(stdhttp.Pusher)
				direct := mode == "direct"
				if flush != direct || hijack != direct || push != direct {
					t.Fatal("invented or hidden native capability")
				}
				// ReaderFrom must reach capture rather than the native copy path.
				n, err := w.(io.ReaderFrom).ReadFrom(io.LimitReader(bytes.NewBufferString("prefix"), 6))
				if n != 6 || err != nil || native.body.Len() != 0 {
					t.Fatal("ReaderFrom bypassed capture", n, err)
				}
				controller := stdhttp.NewResponseController(w)
				if _, _, err := controller.Hijack(); !errors.Is(err, stdhttp.ErrNotSupported) || native.hijacks != 0 {
					t.Fatal("hijack bypassed pending response bytes", err)
				}
				err = controller.SetWriteDeadline(deadline)
				if mode == "opaque" {
					if !errors.Is(err, stdhttp.ErrNotSupported) {
						t.Fatal("invented native deadline", err)
					}
					if err = controller.Flush(); !errors.Is(err, stdhttp.ErrNotSupported) || native.body.Len() != 0 {
						t.Fatal("unsupported flush discarded capture", err)
					}
					return
				}
				if err != nil || !native.deadline.Equal(deadline) {
					t.Fatal("deadline failed to reach native writer", err)
				}
				if err = controller.Flush(); err != nil || native.body.String() != "prefix" || native.flushes != 1 {
					t.Fatal("flush bypassed buffered prefix", native.body.String(), err)
				}
				if direct {
					if err = w.(stdhttp.Pusher).Push("/asset", nil); err != nil || native.pushes != 1 {
						t.Fatal(err)
					}
				}
				if _, _, err = controller.Hijack(); err != nil || native.hijacks != 1 {
					t.Fatal("native hijack was not retained", err)
				}
			})
			handler.ServeHTTP(supplied, httptest.NewRequest("GET", "/", nil))
			if mode == "opaque" && (native.header.Get("ETag") == "" || native.body.String() != "prefix") {
				t.Fatal("unsupported controls changed completed response")
			}
			if mode != "opaque" && native.header.Get("ETag") != "" {
				t.Fatal("flushed response acquired automatic ETag")
			}
		})
	}
}

func TestUnsupportedFlushPreservesEveryNestedBodyOwner(t *testing.T) {
	for _, transparent := range []bool{false, true} {
		native := &etagControlTransport{header: make(stdhttp.Header)}
		var supplied stdhttp.ResponseWriter = etagOpaqueTransport{native}
		if transparent {
			supplied = etagTransparentTransport{supplied}
		}
		inner := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			_, _ = io.WriteString(w, "prefix")
			if err := stdhttp.NewResponseController(w).Flush(); !errors.Is(err, stdhttp.ErrNotSupported) {
				t.Fatal("nested owner invented flush support", err)
			}
			if native.body.Len() != 0 || native.status != 0 {
				t.Fatal("unsupported flush committed a nested response")
			}
			_, _ = io.WriteString(w, "suffix")
		})
		// Independent global/route wrappers still retain native capability rules.
		outer := etagTestHandler(t, DefaultETagConfig(), inner.ServeHTTP)
		outer.ServeHTTP(supplied, httptest.NewRequest("GET", "/", nil))
		if native.body.String() != "prefixsuffix" || native.header.Get("ETag") == "" || native.flushes != 0 {
			t.Fatal("nested unsupported flush damaged the completed response", native.header, native.body.String())
		}
	}
}
