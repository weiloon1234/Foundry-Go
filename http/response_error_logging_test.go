package http

import (
	"bytes"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

type privateResponseError struct {
	mode    string
	formats atomic.Int32
}

func (e *privateResponseError) Error() string {
	e.formats.Add(1)
	switch e.mode {
	case "panic":
		panic("private transfer detail")
	case "goexit":
		runtime.Goexit()
	}
	return "private transfer detail"
}

type failingResponseWriter struct {
	*httptest.ResponseRecorder
	cause error
}

func (w failingResponseWriter) Write([]byte) (int, error) { return 0, w.cause }

func TestResponseMiddlewareLogsWithoutFormattingTransportCauses(t *testing.T) {
	for _, kind := range []string{"compression", "etag"} {
		for _, site := range []string{"reader", "writer"} {
			for _, mode := range []string{"text", "panic", "goexit"} {
				t.Run(kind+"-"+site+"-"+mode, func(t *testing.T) {
					cause := &privateResponseError{mode: mode}
					fail := true
					underlying := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
						w.Header().Set("Content-Type", "text/plain")
						if fail && site == "reader" {
							_, err := io.Copy(w, compressionReaderFunc(func([]byte) (int, error) { return 0, cause }))
							if err != cause {
								t.Error("reader cause was not retained for the caller")
							}
							return
						}
						_, _ = io.WriteString(w, "healthy response")
					})
					compression := DefaultCompressionConfig()
					compression.MinBytes, compression.MaxConcurrent = 0, 1
					etag := DefaultETagConfig()
					etag.MaxConcurrent = 1
					middleware := Compression(compression)
					if kind == "etag" {
						middleware = ETags(etag)
					}
					handler, err := ApplyMiddleware(underlying, middleware)
					if err != nil {
						t.Fatal(err)
					}
					var logs bytes.Buffer
					request := withRequestLogger(httptest.NewRequest("GET", "/", nil), slog.New(slog.NewTextHandler(&logs, nil)))
					request.Header.Set("Accept-Encoding", "gzip, identity;q=0")
					recorder := httptest.NewRecorder()
					var writer stdhttp.ResponseWriter = recorder
					if site == "writer" {
						writer = failingResponseWriter{recorder, cause}
					}
					expectCompressionAbort(t, handler, writer, request)
					if cause.formats.Load() != 0 || strings.Contains(logs.String(), "private") || !strings.Contains(logs.String(), "could not finish") {
						t.Fatal("transport logging formatted, exposed or lost the diagnostic")
					}
					fail = false
					recorder = httptest.NewRecorder()
					handler.ServeHTTP(recorder, request)
					if recorder.Code != 200 {
						t.Fatal("failed transfer retained middleware capacity")
					}
					if kind == "compression" && recorder.Header().Get("Content-Encoding") != "gzip" {
						t.Fatal("compression permit was not released")
					}
					if kind == "etag" && recorder.Header().Get("ETag") == "" {
						t.Fatal("ETag capture permit was not released")
					}
				})
			}
		}
	}
}
