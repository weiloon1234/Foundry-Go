package http

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

func TestCompressedFlushIsReadableBeforeHandlerReturns(t *testing.T) {
	for _, coding := range []string{"gzip", "br"} {
		t.Run(coding, func(t *testing.T) {
			config := DefaultCompressionConfig()
			config.MinBytes = 0
			release := make(chan struct{})
			var releaseOnce sync.Once
			handler := compressionTestHandler(t, config, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				if _, err := io.WriteString(w, "first line\n"); err != nil {
					t.Error(err)
					return
				}
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Error(err)
					return
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "last line\n")
			})
			server := httptest.NewServer(handler)
			defer server.Close()
			defer releaseOnce.Do(func() { close(release) })
			client := server.Client()
			client.Timeout = 3 * time.Second
			request, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
			request.Header.Set("Accept-Encoding", coding)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.Header.Get("Content-Encoding") != coding {
				t.Fatal("stream not encoded")
			}
			var reader io.Reader = response.Body
			if coding == "gzip" {
				gz, err := gzip.NewReader(reader)
				if err != nil {
					t.Fatal(err)
				}
				defer gz.Close()
				reader = gz
			} else {
				reader = brotli.NewReader(reader)
			}
			buffered := bufio.NewReader(reader)
			line, err := buffered.ReadString('\n')
			if err != nil || line != "first line\n" {
				t.Fatalf("flush not readable before return: %q %v", line, err)
			}
			releaseOnce.Do(func() { close(release) })
			tail, err := io.ReadAll(buffered)
			if err != nil || string(tail) != "last line\n" {
				t.Fatalf("stream finalization failed: %q %v", tail, err)
			}
		})
	}
}

func TestCompressionSaturationFallsBackOrReturns503AndReleasesPermit(t *testing.T) {
	config := DefaultCompressionConfig()
	config.MinBytes = 0
	config.MaxConcurrent = 1
	release := make(chan struct{})
	var releaseOnce sync.Once
	handler := compressionTestHandler(t, config, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "response body")
		if r.URL.Path == "/held" {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Error(err)
				return
			}
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	defer releaseOnce.Do(func() { close(release) })
	client := server.Client()
	client.Timeout = 3 * time.Second
	call := func(path, accept string) *http.Response {
		t.Helper()
		request, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL+path, nil)
		request.Header.Set("Accept-Encoding", accept)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	first := call("/held", "gzip")
	defer first.Body.Close()
	if first.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("first request did not hold an encoder")
	}
	second := call("/fallback", "gzip")
	body, err := io.ReadAll(second.Body)
	second.Body.Close()
	if err != nil || second.StatusCode != 200 || second.Header.Get("Content-Encoding") != "" || string(body) != "response body" {
		t.Fatal("saturated encoder did not fall back")
	}
	third := call("/required", "gzip,identity;q=0")
	body, err = io.ReadAll(third.Body)
	third.Body.Close()
	if err != nil || third.StatusCode != 503 || !strings.Contains(string(body), "\"error_code\":\"unavailable\"") {
		t.Fatalf("saturated required coding: %d %s %v", third.StatusCode, body, err)
	}
	releaseOnce.Do(func() { close(release) })
	_, _ = io.Copy(io.Discard, first.Body)
	first.Body.Close()
	fourth := call("/released", "gzip")
	defer fourth.Body.Close()
	body, err = io.ReadAll(fourth.Body)
	if err != nil || fourth.Header.Get("Content-Encoding") != "gzip" || string(decodeCompressed(t, "gzip", body)) != "response body" {
		t.Fatal("encoder permit leaked")
	}
}

func TestCompressionHEADAnd304MatchSelectedGETMetadata(t *testing.T) {
	for _, status := range []int{200, 304} {
		for _, method := range []string{"HEAD", "GET"} {
			if status == 200 && method == "GET" {
				continue
			}
			handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", "8192")
				w.Header().Set("Etag", "\"identity-validator\"")
				w.Header().Set("Content-Digest", "identity-digest")
				w.WriteHeader(status)
			})
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/", nil)
			r.Header.Set("Accept-Encoding", "gzip")
			handler.ServeHTTP(w, r)
			response := w.Result()
			if response.StatusCode != status || response.Header.Get("Content-Length") != "" || response.Header.Get("Content-Encoding") != "gzip" ||
				response.Header.Get("Etag") != "W/\"identity-validator\"" || response.Header.Get("Content-Digest") != "" || w.Body.Len() != 0 {
				t.Fatalf("unwritten representation metadata mismatch: %s %d %v", method, status, response.Header)
			}
		}
	}
	for _, contentType := range []string{"", "application/json"} {
		handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			w.Header().Set("Etag", "\"unseen\"")
			w.WriteHeader(200)
		})
		w := httptest.NewRecorder()
		r := httptest.NewRequest("HEAD", "/", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		handler.ServeHTTP(w, r)
		if w.Header().Get("Content-Length") != "" || w.Header().Get("Etag") != "" || w.Body.Len() != 0 {
			t.Fatal("HEAD fabricated unseen GET metadata")
		}
	}
	// Ordinary identity HEAD keeps its declared length; writes remain bodyless.
	handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("HEAD", "/", nil))
	if w.Header().Get("Content-Length") != "5" || w.Body.Len() != 0 {
		t.Fatal("identity HEAD changed")
	}
}

type compressionControlWriter struct {
	*compressionPlainWriter
	flushes, pushes, hijacks int
	writeDeadline            time.Time
	peer                     net.Conn
}

func (w *compressionControlWriter) Flush()                               { w.flushes++ }
func (w *compressionControlWriter) Push(string, *http.PushOptions) error { w.pushes++; return nil }
func (w *compressionControlWriter) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	return nil
}
func (w *compressionControlWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks++
	conn, peer := net.Pipe()
	w.peer = peer
	return conn, bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), nil
}

type compressionTransparent struct{ http.ResponseWriter }

func (w compressionTransparent) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestCompressionResponseControllerAndUpgradeOwnership(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		base := &compressionControlWriter{compressionPlainWriter: &compressionPlainWriter{header: make(http.Header)}}
		var underlying http.ResponseWriter = base
		if hidden {
			underlying = compressionTransparent{base}
		}
		var accepted net.Conn
		handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
			controller := http.NewResponseController(w)
			deadline := time.Now().Add(time.Minute)
			if err := controller.SetWriteDeadline(deadline); err != nil || !base.writeDeadline.Equal(deadline) {
				t.Error("native deadline was hidden")
			}
			var err error
			accepted, _, err = controller.Hijack()
			if err != nil {
				t.Error(err)
			}
		})
		handler.ServeHTTP(underlying, httptest.NewRequest("GET", "/", nil))
		if accepted == nil || base.hijacks != 1 || base.status != 0 {
			t.Fatal("hijack ownership changed")
		}
		accepted.Close()
		base.peer.Close()
	}
	base := &compressionControlWriter{compressionPlainWriter: &compressionPlainWriter{header: make(http.Header)}}
	handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
		if w != base {
			t.Error("upgrade did not retain native writer")
		}
		_ = w.(http.Pusher).Push("/asset", nil)
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Connection", "keep-alive, Upgrade")
	r.Header.Set("Upgrade", "websocket")
	handler.ServeHTTP(base, r)
	if base.pushes != 1 {
		t.Fatal("upgrade writer changed")
	}

	base = &compressionControlWriter{compressionPlainWriter: &compressionPlainWriter{header: make(http.Header)}}
	config := DefaultCompressionConfig()
	config.MinBytes = 1
	handler = compressionTestHandler(t, config, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("compressed"))
		if _, _, err := http.NewResponseController(w).Hijack(); !errors.Is(err, http.ErrNotSupported) {
			t.Error("hijacked an active compressed stream")
		}
	})
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	handler.ServeHTTP(compressionTransparent{base}, r)
	if base.hijacks != 0 {
		t.Fatal("controller bypassed active compression")
	}
}

type compressionFailWriter struct {
	header  http.Header
	calls   int
	failure error
}

func (w *compressionFailWriter) Header() http.Header       { return w.header }
func (w *compressionFailWriter) WriteHeader(int)           {}
func (w *compressionFailWriter) Write([]byte) (int, error) { w.calls++; return 0, w.failure }

func TestCompressionCancellationFailureAndPrefixBounds(t *testing.T) {
	config := DefaultCompressionConfig()
	ctx, cancel := context.WithCancel(t.Context())
	request := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	preferences, _ := parseAcceptEncoding([]string{"gzip"})
	base := &compressionPlainWriter{header: make(http.Header)}
	response := &compressionResponse{underlying: base, request: request, header: make(http.Header), config: config, preferences: preferences, permits: make(chan struct{}, 1), declaredLength: -1}
	response.Header().Set("Content-Type", "text/plain")
	if _, err := response.Write(bytes.Repeat([]byte("x"), config.MinBytes-1)); err != nil {
		t.Fatal(err)
	}
	if base.status != 0 || base.body.Len() != 0 || cap(response.buffer) > config.MinBytes {
		t.Fatal("prefix buffering escaped its bound")
	}
	cancel()
	if _, err := response.Write([]byte("more")); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled write continued")
	}
	if err := response.flush(); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled flush continued")
	}
	if err := response.finish(); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled response finalized")
	}
	if base.status != 0 || base.body.Len() != 0 {
		t.Fatal("cancellation published buffered output")
	}

	failure := errors.New("write failure")
	failed := &compressionFailWriter{header: make(http.Header), failure: failure}
	response = &compressionResponse{underlying: failed, request: httptest.NewRequest("GET", "/", nil), header: make(http.Header), config: config, preferences: preferences, permits: make(chan struct{}, 1), declaredLength: -1}
	response.Header().Set("Content-Type", "text/plain")
	_, err := response.Write(bytes.Repeat([]byte("x"), 4096))
	if err == nil {
		err = response.finish()
	}
	if !errors.Is(err, failure) {
		t.Fatalf("write failure lost: %v", err)
	}
	calls := failed.calls
	if _, err := response.Write([]byte("more")); !errors.Is(err, failure) && response.err != nil {
		t.Fatal("sticky failure lost")
	}
	if failed.calls != calls {
		t.Fatal("write retried after failure")
	}
	response.release()
	if len(response.permits) != 0 {
		t.Fatal("failed stream retained permit")
	}
}

type compressionDiscardWriter struct{ header http.Header }

func (w *compressionDiscardWriter) Header() http.Header       { return w.header }
func (*compressionDiscardWriter) WriteHeader(int)             {}
func (*compressionDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }
func BenchmarkCompressionBoundedMemory(b *testing.B) {
	for _, coding := range []string{"gzip", "br"} {
		for _, size := range []int{64 << 10, 16 << 20} {
			b.Run(coding+"/"+strconv.Itoa(size), func(b *testing.B) {
				payload := bytes.Repeat([]byte("x"), size)
				config := DefaultCompressionConfig()
				handler, err := ApplyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write(payload)
				}), Compression(config))
				if err != nil {
					b.Fatal(err)
				}
				request := httptest.NewRequest("GET", "/", nil)
				request.Header.Set("Accept-Encoding", coding)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					handler.ServeHTTP(&compressionDiscardWriter{header: make(http.Header)}, request)
				}
			})
		}
	}
}

type compressionFlushFailureWriter struct {
	*compressionControlWriter
	failure    error
	failWrites bool
	failFlush  bool
}

func (w *compressionFlushFailureWriter) Write(data []byte) (int, error) {
	if w.failWrites {
		return 0, w.failure
	}
	return w.compressionControlWriter.Write(data)
}

func (w *compressionFlushFailureWriter) FlushError() error {
	if w.failFlush {
		return w.failure
	}
	w.compressionControlWriter.Flush()
	return nil
}

func TestCompressionResponseControllerRetainsFlushFailures(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		for _, source := range []string{"native", "encoder"} {
			t.Run(fmt.Sprintf("%s/hidden=%t", source, hidden), func(t *testing.T) {
				failure := errors.New("private transport flush failure")
				base := &compressionFlushFailureWriter{
					compressionControlWriter: &compressionControlWriter{
						compressionPlainWriter: &compressionPlainWriter{header: make(http.Header)},
					},
					failure: failure,
				}
				var underlying http.ResponseWriter = base
				if hidden {
					underlying = compressionTransparent{base}
				}
				config := DefaultCompressionConfig()
				config.Encoders = []CompressionEncoder{GzipCompression(GzipFastest)}
				config.MinBytes = 0
				accept := []string(nil)
				if source == "encoder" {
					accept = []string{"gzip"}
				}
				preferences, err := parseAcceptEncoding(accept)
				if err != nil {
					t.Fatal(err)
				}
				response := &compressionResponse{
					underlying:     underlying,
					request:        httptest.NewRequest("GET", "/", nil),
					header:         make(http.Header),
					config:         config,
					preferences:    preferences,
					permits:        make(chan struct{}, 1),
					declaredLength: -1,
				}
				defer response.release()
				wrapped := responseCapabilities(response)
				wrapped.Header().Set("Content-Type", "text/plain")
				if _, err := wrapped.Write([]byte("buffered stream content")); err != nil {
					t.Fatal(err)
				}
				if source == "native" {
					base.failFlush = true
				} else {
					base.failWrites = true
				}
				if err := http.NewResponseController(wrapped).Flush(); !errors.Is(err, failure) {
					t.Fatalf("controller lost flush failure: %v", err)
				}
				if n, err := wrapped.Write([]byte("later")); n != 0 || !errors.Is(err, failure) {
					t.Fatalf("flush failure was not sticky: n=%d, err=%v", n, err)
				}
				if err := response.finish(); !errors.Is(err, failure) {
					t.Fatalf("finish lost flush failure: %v", err)
				}
			})
		}
	}
}

func TestCompression304PreservesValidatorsWithoutBodyMetadata(t *testing.T) {
	for _, media := range []string{"", "application/json"} {
		for _, coding := range []string{"identity", "gzip", "br"} {
			handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
				if media != "" {
					w.Header().Set("Content-Type", media)
				}
				w.Header().Set("ETag", "\"version-1\"")
				w.WriteHeader(http.StatusNotModified)
			})
			request := httptest.NewRequest("GET", "/", nil)
			request.Header.Set("Accept-Encoding", coding)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			tag := "\"version-1\""
			if coding != "identity" {
				tag = "W/" + tag
			}
			if recorder.Code != 304 || recorder.Body.Len() != 0 || recorder.Header().Get("ETag") != tag {
				t.Fatal("conditional validator disappeared", media, coding, recorder.Header())
			}
			if recorder.Header().Get("Content-Length") != "" || recorder.Header().Get("Content-Encoding") != "" {
				t.Fatal("unknown representation metadata was invented", recorder.Header())
			}
			assertCORSVary(t, recorder.Header(), "accept-encoding")
		}
	}
}
