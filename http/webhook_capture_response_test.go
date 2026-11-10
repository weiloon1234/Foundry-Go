package http

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
)

type captureResponseTransport struct {
	*httptest.ResponseRecorder
	readFroms     int
	failed        bool
	hijacks       int
	informational []int
}

var captureWriteError = errors.New("private write error")

// ResponseRecorder treats its first 1xx as final. Model native informational
// commits here and also exercise real 1xx transport in the socket test.
func (w *captureResponseTransport) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != 101 {
		w.informational = append(w.informational, status)
		return
	}
	w.ResponseRecorder.WriteHeader(status)
}

func (w *captureResponseTransport) Write(p []byte) (int, error) {
	if w.failed {
		n, _ := w.ResponseRecorder.Write(p[:min(2, len(p))])
		return n, captureWriteError
	}
	return w.ResponseRecorder.Write(p)
}
func (w *captureResponseTransport) ReadFrom(r io.Reader) (int64, error) {
	w.readFroms++
	return io.Copy(struct{ io.Writer }{w}, r)
}
func (w *captureResponseTransport) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks++
	return nil, nil, nil
}

func TestWebhookCaptureResponseCapabilities(t *testing.T) {
	for _, mode := range []string{"write", "readfrom", "flush", "head", "informational", "hijack", "failed-write", "failed-readfrom"} {
		t.Run(mode, func(t *testing.T) {
			recorder := captureTestRecorder(t)
			native := &captureResponseTransport{ResponseRecorder: httptest.NewRecorder(), failed: strings.HasPrefix(mode, "failed-")}
			data := strings.Repeat("response", 800)
			handler := captureTestHandler(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				if _, ok := w.(stdhttp.Hijacker); !ok {
					t.Error("hijack capability lost")
				}
				w.Header().Set("Content-Type", "text/plain")
				w.Header().Set("Content-Encoding", "identity")
				switch mode {
				case "hijack":
					_, _, err := stdhttp.NewResponseController(w).Hijack()
					if err != nil {
						t.Error(err)
					}
					return
				case "informational":
					w.WriteHeader(103)
					w.WriteHeader(102)
					w.WriteHeader(201)
				case "flush":
					w.(stdhttp.Flusher).Flush()
				case "readfrom", "failed-readfrom":
					n, err := w.(io.ReaderFrom).ReadFrom(io.LimitReader(strings.NewReader(data), int64(len(data))))
					if native.failed {
						if n != 2 || err != captureWriteError {
							t.Error("copy failure changed")
						}
					} else if n != int64(len(data)) || err != nil {
						t.Error("copy changed", n, err)
					}
					return
				}
				_, _ = io.WriteString(w, data)
				w.Header().Set("Content-Type", "mutated-after-commit")
			}), DefaultServerConfig(), recorder)
			method := "POST"
			if mode == "head" {
				method = "HEAD"
			}
			handler.ServeHTTP(native, httptest.NewRequest(method, "/wh", nil))
			s := captureTestTake(t, recorder)
			c := s.Completion
			if mode == "hijack" {
				if !c.Hijacked || !c.Status.IsNull() || len(c.ResponseHead) != 0 || native.hijacks != 1 {
					t.Fatal("hijack capture invented response")
				}
				return
			}
			if c.ResponseContentType != "text/plain" || c.ResponseEncoding != "identity" {
				t.Fatal("committed response metadata changed")
			}
			status, ok := c.Status.Get()
			wantStatus := uint16(200)
			if mode == "informational" {
				wantStatus = 201
			}
			if !ok || status != wantStatus {
				t.Fatal("incorrect final status", status)
			}
			switch {
			case mode == "head":
				if len(c.ResponseHead) != 0 || c.ResponseTruncated {
					t.Fatal("HEAD captured suppressed entity bytes")
				}
			case native.failed:
				if string(c.ResponseHead) != data[:2] || c.ResponseBytes != 2 || !c.WriteFailed || c.ResponseTruncated {
					t.Fatal("preview included rejected bytes")
				}
			default:
				if string(c.ResponseHead) != data[:httpcapture.ResponseBytes] || !c.ResponseTruncated || c.ResponseBytes != int64(len(data)) {
					t.Fatal("response prefix/count mismatch")
				}
			}
			if mode == "readfrom" && native.readFroms != 1 {
				t.Fatal("native fast path was not resumed after the bounded head")
			}
			if mode == "informational" && len(native.informational) != 2 {
				t.Fatal("informational headers were swallowed")
			}
			if mode == "flush" && !native.Flushed {
				t.Fatal("flush lost")
			}
		})
	}
}

func TestWebhookCaptureReadFromShortAndSourceError(t *testing.T) {
	for _, fail := range []bool{false, true} {
		native := httptest.NewRecorder()
		observed := &observedResponse{native: native, head: &captureResponse{}}
		source := &captureTerminalReader{data: []byte("short"), terminal: io.EOF}
		if fail {
			source.terminal = captureWriteError
		}
		n, err := observed.ReadFrom(source)
		if n != 5 || string(observed.head.prefix) != "short" || native.Body.String() != "short" || observed.head.truncated {
			t.Fatal("short copy changed")
		}
		if fail && (err != captureWriteError || observed.err != err) || !fail && err != nil {
			t.Fatal("copy error changed")
		}
	}
}

type captureTerminalReader struct {
	data     []byte
	terminal error
	calls    int
}

func (r *captureTerminalReader) Read(p []byte) (int, error) {
	r.calls++
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.terminal
	}
	return n, nil
}
func (r *captureTerminalReader) Close() error { return nil }

func TestWebhookCapturePrereadTerminalIdentity(t *testing.T) {
	for _, terminal := range []error{io.EOF, &stdhttp.MaxBytesError{Limit: 17}, io.ErrUnexpectedEOF, captureWriteError} {
		source := &captureTerminalReader{data: []byte("12345678901234567"), terminal: terminal}
		c := &kernelCapture{limit: 1024}
		body := c.preread(source, -1)
		var got bytes.Buffer
		buffer := make([]byte, 3)
		for {
			n, err := body.Read(buffer)
			got.Write(buffer[:n])
			if err != nil {
				if err != terminal {
					t.Fatal("terminal identity changed")
				}
				break
			}
		}
		if n, err := body.Read(buffer); n != 0 || err != terminal || source.calls != 1 || got.String() != "12345678901234567" {
			t.Fatal("terminal offset/stickiness changed")
		}
		_ = body.Close()
	}
}

func TestWebhookCaptureCompletenessRequiresEOF(t *testing.T) {
	for _, size := range []int{1024, 1025, 2000} {
		recorder := captureTestRecorder(t)
		source := &captureTrackingBody{Reader: bytes.NewReader(bytes.Repeat([]byte(" "), size))}
		r := httptest.NewRequest("POST", "/wh", nil)
		r.Body = source
		r.ContentLength = -1
		handler := captureTestHandler(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) }), DefaultServerConfig(), recorder)
		handler.ServeHTTP(httptest.NewRecorder(), r)
		s := captureTestTake(t, recorder)
		if s.Body.ObservedBytes != int64(min(size, 1025)) {
			t.Fatal("preread drained past cap+1")
		}
		if size == 1024 {
			if s.Body.End != httpcapture.EOF || s.Body.MeasuredBytes.IsNull() {
				t.Fatal("exact-cap EOF probe missing")
			}
		} else if s.Body.End != httpcapture.OverCaptureLimit || !s.Body.MeasuredBytes.IsNull() {
			t.Fatal("length/parse success was mistaken for EOF")
		}
	}
}

func TestWebhookCapturePrereadAllocationAndUntrustedLength(t *testing.T) {
	for _, declared := range []int64{-1, 1, 512} {
		data := bytes.Repeat([]byte("a"), 512)
		source := &captureTrackingBody{Reader: bytes.NewReader(data)}
		c := &kernelCapture{limit: 2 << 20}
		body := c.preread(source, declared)
		got, err := io.ReadAll(body)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("declared length changed replay", err)
		}
		_ = body.Close()
		s := c.replay.snapshot(c.limit)
		if !bytes.Equal(s.Prefix, data) || s.End != httpcapture.EOF {
			t.Fatal("growth lost preread data")
		}
		if declared != 1 && cap(s.Prefix) > bodyPresize {
			t.Fatal("small body retained the full reservation")
		}
	}
}
