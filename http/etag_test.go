package http

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func etagTestHandler(t *testing.T, config ETagConfig, next stdhttp.HandlerFunc) stdhttp.Handler {
	t.Helper()
	handler, err := ApplyMiddleware(next, ETags(config))
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
func etagTestRequest(handler stdhttp.Handler, method, condition, tag string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/", nil)
	if condition != "" {
		request.Header.Set(condition, tag)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestETagNativeConditionsRetainRepresentationAndPolicy(t *testing.T) {
	var calls atomic.Int32
	payload := "hello"
	handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=0")
		w.Header().Set("X-Policy", "retained")
		w.Header().Add("Vary", "Origin")
		w.Header().Add("Set-Cookie", "session=fixture; HttpOnly")
		_, _ = io.WriteString(w, payload)
	})
	first := etagTestRequest(handler, "GET", "", "")
	tag := first.Header().Get("ETag")
	if first.Code != 200 || first.Body.String() != payload || tag == "" || EntityTag(tag).Validate() != nil {
		t.Fatal("missing complete representation or validator", first.Code, tag)
	}
	if first.Header().Get("Accept-Ranges") != "" {
		t.Fatal("middleware invented byte-range support")
	}
	for _, condition := range []string{tag, "W/" + tag, "\"different\", W/" + tag, "*"} {
		response := etagTestRequest(handler, "GET", "If-None-Match", condition)
		if response.Code != 304 || response.Body.Len() != 0 || response.Header().Get("ETag") != tag {
			t.Fatal("conditional response", condition, response.Code, response.Header(), response.Body.String())
		}
		if response.Header().Get("X-Policy") != "retained" || len(response.Header().Values("Set-Cookie")) != 1 ||
			response.Header().Get("Cache-Control") != "private, max-age=0" || response.Header().Get("Vary") != "Origin" {
			t.Fatal("304 discarded policy headers", response.Header())
		}
	}
	response := etagTestRequest(handler, "GET", "If-Match", "\"different\"")
	if response.Code != 412 || !strings.Contains(response.Body.String(), string(PreconditionFailed)) {
		t.Fatal("condition failure did not use shared HTTP errors", response.Code, response.Body.String())
	}
	response = etagTestRequest(handler, "GET", "If-None-Match", "\"different\"")
	if response.Code != 200 || response.Body.String() != payload {
		t.Fatal("nonmatching validator discarded body")
	}
	payload = "changed"
	response = etagTestRequest(handler, "GET", "If-None-Match", tag)
	if response.Code != 200 || response.Header().Get("ETag") == tag || response.Body.String() != payload {
		t.Fatal("changed body retained old validator")
	}
	if calls.Load() != 8 {
		t.Fatal("handler did not run exactly once per request", calls.Load())
	}
}

func TestETagCaptureBoundariesAndFlush(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parts []string
		flush bool
		tag   bool
	}{
		{"empty", nil, false, true},
		{"exact", []string{"ab", "cd"}, false, true},
		{"overflow", []string{"ab", "cde", "f"}, false, false},
		{"flush", []string{"ab", "cd"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultETagConfig()
			config.MaxBytes = 4
			handler := etagTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				for i, part := range tc.parts {
					_, _ = io.WriteString(w, part)
					if tc.flush && i == 0 {
						if err := stdhttp.NewResponseController(w).Flush(); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
			response := etagTestRequest(handler, "GET", "", "")
			if response.Body.String() != strings.Join(tc.parts, "") || (response.Header().Get("ETag") != "") != tc.tag {
				t.Fatal("captured prefix or validator diverged", response.Body.String(), response.Header())
			}
			if tc.flush && !response.Flushed {
				t.Fatal("flush did not reach transport before return")
			}
		})
	}
}

func TestETagPreservesMethodsExistingValidatorsAndIneligibleResponses(t *testing.T) {
	for _, tc := range []struct {
		name, method, header, value string
		status                      int
	}{
		{"post", "POST", "", "", 200}, {"head", "HEAD", "", "", 200},
		{"existing", "GET", "ETag", "\"manual\"", 200}, {"range", "GET", "Range", "bytes=0-1", 200},
		{"failure", "GET", "", "", 500}, {"partial", "GET", "Content-Range", "bytes 0-1/5", 206},
		{"no-store", "GET", "Cache-Control", "no-store", 200}, {"events", "GET", "Content-Type", "text/event-stream", 200},
		{"trailers", "GET", "Trailer", "X-Final", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				calls++
				if tc.header != "" && tc.header != "Range" {
					w.Header().Set(tc.header, tc.value)
				}
				w.WriteHeader(tc.status)
				if r.Method != "HEAD" {
					_, _ = io.WriteString(w, "complete")
				}
				w.Header().Set("X-Final", "done")
			})
			request := httptest.NewRequest(tc.method, "/", nil)
			request.Header.Set("If-None-Match", "*")
			if tc.header == "Range" {
				request.Header.Set(tc.header, tc.value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			wantTag := ""
			if tc.header == "ETag" {
				wantTag = tc.value
			}
			if calls != 1 || response.Code != tc.status || response.Header().Get("ETag") != wantTag {
				t.Fatal("native behavior changed", calls, response.Code, response.Header())
			}
			if tc.header == "Trailer" && response.Result().Trailer.Get("X-Final") != "done" {
				t.Fatal("trailer value lost")
			}
		})
	}
}

func TestETagSnapshotsFinalHeadersAndPreservesLateTrailers(t *testing.T) {
	handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("X-Choice", "before")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "body")
		w.Header().Set("X-Choice", "late")
		w.Header().Set(stdhttp.TrailerPrefix+"X-Final", "finished")
	})
	response := etagTestRequest(handler, "GET", "", "")
	if response.Result().Header.Get("X-Choice") != "before" || response.Header().Get("ETag") != "" ||
		response.Result().Trailer.Get("X-Final") != "finished" || response.Body.String() != "body" {
		t.Fatal("header boundary or trailers changed", response.Result().Header, response.Result().Trailer)
	}
}

func TestETagCaptureSaturationAndRelease(t *testing.T) {
	ready, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	config := DefaultETagConfig()
	config.MaxConcurrent = 1
	config.AdmissionWait = 10 * time.Millisecond
	large := strings.Repeat("x", responseBufferPageBytes+1)
	var calls atomic.Int32
	handler := etagTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		n := calls.Add(1)
		if r.URL.Path == "/small" {
			_, _ = io.WriteString(w, "body")
			return
		}
		_, _ = io.WriteString(w, large)
		if n == 1 {
			close(ready)
			<-release
		}
	})
	first := httptest.NewRecorder()
	go func() { defer close(done); handler.ServeHTTP(first, httptest.NewRequest("GET", "/", nil)) }()
	<-ready
	second := etagTestRequest(handler, "GET", "", "")
	// Small captures never compete for large-capture slots.
	small := httptest.NewRecorder()
	handler.ServeHTTP(small, httptest.NewRequest("GET", "/small", nil))
	close(release)
	<-done
	third := etagTestRequest(handler, "GET", "", "")
	if second.Body.String() != large || second.Header().Get("ETag") != "" || small.Header().Get("ETag") == "" ||
		first.Header().Get("ETag") == "" || third.Header().Get("ETag") == "" {
		t.Fatal("saturation lost body or capture permit was retained")
	}
}

func TestETagWaitsBrieflyForCaptureCapacity(t *testing.T) {
	config := DefaultETagConfig()
	config.MaxConcurrent = 1
	config.AdmissionWait = 2 * time.Second
	large := strings.Repeat("y", responseBufferPageBytes+1)
	entered := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Bool
	handler := etagTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = io.WriteString(w, large)
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	})
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- etagTestRequest(handler, "GET", "", "") }()
	<-entered
	waiting := make(chan *httptest.ResponseRecorder)
	go func() { waiting <- etagTestRequest(handler, "GET", "", "") }()
	time.Sleep(20 * time.Millisecond)
	close(release)
	if (<-done).Header().Get("ETag") == "" || (<-waiting).Header().Get("ETag") == "" {
		t.Fatal("bounded capacity wait did not admit the queued capture")
	}
}

func TestETagValidatesDeclaredSingleWriteWithoutCapture(t *testing.T) {
	config := DefaultETagConfig()
	config.MaxConcurrent = 1
	config.AdmissionWait = 0
	body := strings.Repeat("z", 3*responseBufferPageBytes)
	handler := etagTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if n, err := w.Write([]byte(body)); err != nil || n != len(body) {
			t.Errorf("single write=%d %v", n, err)
		}
		if _, err := w.Write([]byte("extra")); !errors.Is(err, stdhttp.ErrContentLength) {
			t.Errorf("write after complete body=%v", err)
		}
	})
	response := etagTestRequest(handler, "GET", "", "")
	tag := response.Header().Get("ETag")
	if response.Code != 200 || tag == "" || response.Body.String() != body {
		t.Fatalf("direct validation: %d %q", response.Code, tag)
	}
	if conditional := etagTestRequest(handler, "GET", "If-None-Match", tag); conditional.Code != 304 || conditional.Body.Len() != 0 {
		t.Fatalf("conditional direct validation: %d", conditional.Code)
	}
}

func assertETagAbort(t *testing.T, handler stdhttp.Handler) {
	t.Helper()
	defer func() {
		if got := recover(); got != stdhttp.ErrAbortHandler {
			t.Fatal("incomplete response was not aborted", got)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestETagNeverValidatesObservedIncompleteSources(t *testing.T) {
	sourceErr := errors.New("fixture source failure")
	for _, mode := range []string{"short", "long", "reader", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				switch mode {
				case "short":
					w.Header().Set("Content-Length", "5")
					_, _ = io.WriteString(w, "ab")
				case "long":
					w.Header().Set("Content-Length", "1")
					if _, err := io.WriteString(w, "ab"); !errors.Is(err, stdhttp.ErrContentLength) {
						t.Fatal(err)
					}
				case "reader":
					if _, err := w.(io.ReaderFrom).ReadFrom(etagFailingReader{sourceErr}); !errors.Is(err, sourceErr) {
						t.Fatal(err)
					}
				case "canceled":
					_, _ = io.WriteString(w, "body")
					r.Context().Value(etagCancelKey{}).(context.CancelFunc)()
				}
			})
			if mode == "canceled" {
				native := handler
				handler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					ctx = context.WithValue(ctx, etagCancelKey{}, cancel)
					native.ServeHTTP(w, r.WithContext(ctx))
				})
			}
			assertETagAbort(t, handler)
		})
	}
}

type etagCancelKey struct{}
type etagFailingReader struct{ err error }

func (r etagFailingReader) Read(p []byte) (int, error) { return copy(p, "prefix"), r.err }
