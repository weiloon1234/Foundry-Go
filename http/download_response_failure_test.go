package http

import (
	"context"
	"errors"
	"math"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

type downloadCallbackWriter struct {
	*httptest.ResponseRecorder
	header func()
	status func()
	write  func()
}

func (w downloadCallbackWriter) Header() stdhttp.Header {
	if w.header != nil {
		w.header()
	}
	return w.ResponseRecorder.Header()
}
func (w downloadCallbackWriter) WriteHeader(status int) {
	if w.status != nil {
		w.status()
	}
	w.ResponseRecorder.WriteHeader(status)
}
func (w downloadCallbackWriter) Write(data []byte) (int, error) {
	if w.write != nil {
		w.write()
	}
	return w.ResponseRecorder.Write(data)
}

func TestDownloadOwnsEveryNativeWriterCallbackFailure(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"header", "status", "write"} {
		for _, mode := range []string{"panic", "goexit"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				var closes, calls atomic.Int32
				router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
					return downloadText("abcdefghij", &closes), nil
				})
				fail := func() {
					calls.Add(1)
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("private-writer-data")
				}
				writer := downloadCallbackWriter{ResponseRecorder: httptest.NewRecorder()}
				switch operation {
				case "header":
					writer.header = fail
				case "status":
					writer.status = fail
				case "write":
					writer.write = fail
				}
				defer func() {
					if recovered := recover(); recovered != stdhttp.ErrAbortHandler {
						t.Errorf("native callback escaped: %v", recovered)
					}
					if closes.Load() != 1 || calls.Load() != 1 {
						t.Error("failed writer reused or file leaked", closes.Load(), calls.Load())
					}
				}()
				router.ServeHTTP(writer, httptest.NewRequest("GET", "/file", nil))
			})
		}
	}
}

func TestDownloadCleansUpWhenCanceledAfterSourceReturns(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var closes atomic.Int32
	source := strings.NewReader("payload")
	router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
		return DownloadFrom(func(context.Context) (DownloadContent, error) {
			cancel()
			return DownloadContent{Body: fileReaderCallbacks{read: source.Read, seek: source.Seek, close: func() error { closes.Add(1); return nil }}, MediaType: "text/plain; charset=utf-8"}, nil
		}), nil
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(ctx, "GET", "/file", nil))
	if recorder.Code != 408 || closes.Load() != 1 || recorder.Header().Get("Content-Disposition") != "" {
		t.Fatal("canceled preparation retained body", recorder.Code, closes.Load())
	}
}

func TestDownloadNeverOpensSourceAfterHandlerFailure(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"returned", "canceled", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var opens atomic.Int32
			download := DownloadFrom(func(context.Context) (DownloadContent, error) { opens.Add(1); return DownloadContent{}, nil })
			router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
				switch mode {
				case "canceled":
					cancel()
					return download, nil
				case "panic":
					panic("private-handler-data")
				case "goexit":
					runtime.Goexit()
				}
				return download, NotFound
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequestWithContext(ctx, "GET", "/file", nil))
			expected := 500
			if mode == "returned" {
				expected = 404
			}
			if mode == "canceled" {
				expected = 408
			}
			if recorder.Code != expected || opens.Load() != 0 || strings.Contains(recorder.Body.String(), "private") {
				t.Fatal("failed handler reached source", recorder.Code, opens.Load())
			}
		})
	}
}

func TestDownloadCloseFailuresDoNotReplaceCompletedRepresentation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			var closes atomic.Int32
			reader := strings.NewReader("payload")
			source := DownloadFrom(func(context.Context) (DownloadContent, error) {
				return DownloadContent{MediaType: "text/plain; charset=utf-8", Body: fileReaderCallbacks{read: reader.Read, seek: reader.Seek, close: func() error {
					closes.Add(1)
					switch mode {
					case "panic":
						panic("private-close-data")
					case "goexit":
						runtime.Goexit()
					}
					return errors.New("private-close-data")
				}}}, nil
			})
			router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) { return source, nil })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/file", nil))
			if recorder.Code != 200 || recorder.Body.String() != "payload" || closes.Load() != 1 {
				t.Fatal("close failure replaced completed payload", recorder.Code, closes.Load())
			}
		})
	}
}

func TestDownloadLimitsRejectUnsafeNativeRangeArithmetic(t *testing.T) {
	limits := DefaultFileResponseLimits()
	limits.Bytes = math.MaxInt64
	if limits.Validate() == nil {
		t.Fatal("unsafe sum of native ranges accepted")
	}
	limits = DefaultFileResponseLimits()
	limits.Ranges = 129
	if limits.Validate() == nil {
		t.Fatal("unbounded range count accepted")
	}
}
