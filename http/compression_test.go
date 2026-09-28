package http

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func compressionTestHandler(t *testing.T, config CompressionConfig, handler http.HandlerFunc) http.Handler {
	t.Helper()
	wrapped, err := ApplyMiddleware(handler, Compression(config))
	if err != nil {
		t.Fatal(err)
	}
	return wrapped
}
func decodeCompressed(t *testing.T, coding string, data []byte) []byte {
	t.Helper()
	var reader io.Reader = bytes.NewReader(data)
	switch coding {
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			t.Fatal(err)
		}
		defer gz.Close()
		reader = gz
	case "br":
		reader = brotli.NewReader(reader)
	case "":
	default:
		t.Fatalf("unexpected coding %q", coding)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return output
}
func TestCompressionNegotiatesStreamsAndOwnsHeaders(t *testing.T) {
	body := strings.Repeat("hello 中文 + / structured response\n", 100)
	for _, coding := range []string{"gzip", "br", "br, gzip", "gzip;q=0.5,identity;q=1", ""} {
		config := DefaultCompressionConfig()
		handler := compressionTestHandler(t, config, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Header().Set("Etag", "\"original\"")
			w.Header().Set("Content-Digest", "obsolete")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("X-Snapshot", "before")
			w.WriteHeader(200)
			w.Header().Set("X-Snapshot", "after")
			for _, chunk := range []string{body[:40], body[40:900], body[900:]} {
				if _, err := io.WriteString(w, chunk); err != nil {
					t.Error(err)
				}
			}
		})
		config.Encoders[0] = CompressionEncoder{} // Assembly owns its declaration.
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		if coding != "" {
			r.Header.Set("Accept-Encoding", coding)
		}
		handler.ServeHTTP(w, r)
		response := w.Result()
		data := decodeCompressed(t, response.Header.Get("Content-Encoding"), w.Body.Bytes())
		if w.Code != 200 || string(data) != body || response.Header.Get("X-Snapshot") != "before" {
			t.Fatal("compression changed payload or header snapshot")
		}
		if !strings.Contains(strings.Join(response.Header.Values("Vary"), ","), "Accept-Encoding") || !strings.Contains(strings.Join(response.Header.Values("Vary"), ","), "Origin") {
			t.Fatal("cache variation lost")
		}
		if response.Header.Get("Content-Encoding") != "" {
			if response.Header.Get("Content-Length") != "" || response.Header.Get("Etag") != "W/\"original\"" || response.Header.Get("Content-Digest") != "" {
				t.Fatal("representation metadata was not adjusted")
			}
		} else if response.Header.Get("Content-Length") != strconv.Itoa(len(body)) || response.Header.Get("Etag") != "\"original\"" {
			t.Fatal("identity metadata changed")
		}
	}
}
func TestCompressionSkipsUnsafeAndUnhelpfulTransforms(t *testing.T) {
	body := strings.Repeat("payload ", 500)
	for _, test := range []struct {
		status  int
		headers map[string]string
	}{
		{200, map[string]string{"Cache-Control": "public, no-transform"}},
		{200, map[string]string{"Cache-Control": "private"}},
		{200, map[string]string{"Cache-Control": "no-store"}},
		{200, map[string]string{"Set-Cookie": "session=opaque"}},
		{200, map[string]string{"Content-Type": "text/event-stream"}},
		{200, map[string]string{"Content-Type": "application/grpc"}},
		{200, map[string]string{"Content-Type": "image/png"}},
		{206, map[string]string{"Content-Range": "bytes 0-3999/4000"}},
	} {
		handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			for name, value := range test.headers {
				w.Header().Set(name, value)
			}
			w.WriteHeader(test.status)
			_, _ = w.Write([]byte(body))
		})
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", "br, gzip")
		handler.ServeHTTP(w, r)
		if w.Header().Get("Content-Encoding") != "" || w.Body.String() != body {
			t.Fatalf("excluded representation transformed: %+v", test)
		}
	}
	for _, accept := range []string{"gzip", "gzip,identity;q=0"} {
		handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("tiny")) })
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", accept)
		handler.ServeHTTP(w, r)
		encoded := w.Header().Get("Content-Encoding")
		if (encoded != "") != strings.Contains(accept, "identity;q=0") || string(decodeCompressed(t, encoded, w.Body.Bytes())) != "tiny" {
			t.Fatal("minimum size/identity preference ignored")
		}
	}
}
func TestCompressionPreencodedAndUnacceptableResponses(t *testing.T) {
	var wire bytes.Buffer
	gz := gzip.NewWriter(&wire)
	_, _ = gz.Write([]byte("existing encoded response"))
	_ = gz.Close()
	handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(wire.Bytes())
	})
	for _, accept := range []string{"gzip", "gzip;q=0,br"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", accept)
		handler.ServeHTTP(w, r)
		if accept == "gzip" {
			if !bytes.Equal(w.Body.Bytes(), wire.Bytes()) {
				t.Fatal("preencoded response was recompressed")
			}
		} else if w.Code != 406 || !strings.Contains(w.Body.String(), "\"error_code\":\"not_acceptable\"") {
			t.Fatal("unsupported preencoded response was sent")
		}
	}
	handler = compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private")
		_, _ = w.Write([]byte("private content"))
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "br, identity;q=0")
	handler.ServeHTTP(w, r)
	if w.Code != 406 || strings.Contains(w.Body.String(), "private content") {
		t.Fatal("unacceptable private response escaped")
	}
}
func TestCompressionLengthFailureAbortsInsteadOfFinishingTruncatedStream(t *testing.T) {
	handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "10000")
		_, _ = w.Write([]byte(strings.Repeat("content", 500)))
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Fatalf("incomplete representation did not abort: %v", got)
		}
	}()
	handler.ServeHTTP(w, r)
}

type compressionPlainWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *compressionPlainWriter) Header() http.Header { return w.header }
func (w *compressionPlainWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *compressionPlainWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(p)
}

func TestCompressionOptionalWriterCapabilities(t *testing.T) {
	for _, underlying := range []http.ResponseWriter{&compressionPlainWriter{header: make(http.Header)}, httptest.NewRecorder()} {
		handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
			_, wantFlush := underlying.(http.Flusher)
			_, gotFlush := w.(http.Flusher)
			_, wantHijack := underlying.(http.Hijacker)
			_, gotHijack := w.(http.Hijacker)
			_, wantPush := underlying.(http.Pusher)
			_, gotPush := w.(http.Pusher)
			if wantFlush != gotFlush || wantHijack != gotHijack || wantPush != gotPush {
				t.Error("optional interface set changed")
			}
			if !wantFlush {
				if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrNotSupported) {
					t.Error("unsupported flush changed")
				}
			}
			_, _ = w.Write([]byte("small body"))
		})
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		handler.ServeHTTP(underlying, r)
	}
}
func TestCompressionTrailersAndReadFrom(t *testing.T) {
	body := strings.Repeat("streamed body ", 500)
	handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Trailer", "X-Checksum, Content-Digest")
		// Hide WriterTo to exercise the middleware's ReaderFrom path.
		n, err := io.Copy(w, struct{ io.Reader }{strings.NewReader(body)})
		if err != nil || n != int64(len(body)) {
			t.Error("stream copy failed")
		}
		w.Header().Set("X-Checksum", "domain-checksum")
		w.Header().Set("Content-Digest", "stale-digest")
		w.Header().Set(http.TrailerPrefix+"Digest", "stale-late-digest")
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	handler.ServeHTTP(w, r)
	response := w.Result()
	if string(decodeCompressed(t, response.Header.Get("Content-Encoding"), w.Body.Bytes())) != body {
		t.Fatal("ReaderFrom bypassed compression")
	}
	if response.Trailer.Get("X-Checksum") != "domain-checksum" || response.Trailer.Get("Content-Digest") != "" || response.Trailer.Get("Digest") != "" {
		t.Fatalf("trailers changed incorrectly: %v", response.Trailer)
	}
	if !reflect.DeepEqual(response.Header.Values("Trailer"), []string{"X-Checksum"}) {
		t.Fatal("invalid digest trailer declaration retained")
	}
}
