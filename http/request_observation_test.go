package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func observationBoundary(t *testing.T, config ServerConfig, recorder *observability.Recorder, handler stdhttp.HandlerFunc) stdhttp.Handler {
	t.Helper()
	wrapped := newHandlerLifetime().wrap(handler, slog.New(slog.NewTextHandler(io.Discard, nil)), config)
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		wrapped.ServeHTTP(w, r.WithContext(observability.WithContext(r.Context(), recorder)))
	})
}

func observationRecorder(t *testing.T) *observability.Recorder {
	t.Helper()
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := recorder.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return recorder
}

func TestHTTPTraceBoundaryDoesNotInheritKernelOrUntrustedHeaders(t *testing.T) {
	parent, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := tracing.New(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, trust := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "trusted"}[trust], func(t *testing.T) {
			recorder := observationRecorder(t)
			config := DefaultServerConfig()
			config.TrustTraceContext = trust
			var traces []tracing.Context
			handler := observationBoundary(t, config, recorder, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				traces = append(traces, tracing.FromContext(r.Context()))
				w.WriteHeader(stdhttp.StatusNoContent)
			})
			for range 2 {
				r := httptest.NewRequest("GET", "/private-customer-id", nil)
				ctx, err := tracing.WithContext(r.Context(), parent)
				if err != nil {
					t.Fatal(err)
				}
				r = r.WithContext(ctx)
				r.Header.Set(tracing.ParentHeader, remote.TraceParent())
				r.Header.Set(RequestIDHeader, "forged")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, r)
				if response.Code != 204 || response.Header().Get(RequestIDHeader) == "forged" {
					t.Fatal("HTTP wire correlation changed")
				}
			}
			if traces[0].IsZero() || traces[0].SpanID() == traces[1].SpanID() || traces[0].TraceID() == parent.TraceID() {
				t.Fatal("request inherited kernel trace or reused a span")
			}
			if trust {
				if traces[0].TraceID() != remote.TraceID() || traces[1].TraceID() != remote.TraceID() {
					t.Fatal("trusted trace parent was lost")
				}
			} else if traces[0].TraceID() == remote.TraceID() || traces[0].TraceID() == traces[1].TraceID() {
				t.Fatal("untrusted requests did not get independent traces")
			}
			for _, entry := range recorder.Snapshot().Recent {
				if entry.Result.Outcome != observability.Succeeded || entry.Result.Status != 204 || entry.RequestID == "" || entry.Operation.Name != "request" {
					t.Fatal("incorrect request completion", entry)
				}
				if trust && entry.ParentID != remote.SpanID() {
					t.Fatal("missing remote parent span")
				}
			}
		})
	}
}

func TestHTTPConcurrentBudgetRetainsCancelledHandlerUntilReturn(t *testing.T) {
	recorder := observationRecorder(t)
	config := DefaultServerConfig()
	config.MaxConcurrentRequests = 1
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	handler := observationBoundary(t, config, recorder, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/held" {
			close(entered)
			<-release
		}
		w.WriteHeader(204)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		defer close(exited)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, "GET", "/held", nil))
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not enter")
	}
	cancel()
	response := httptest.NewRecorder()
	waiting, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(waiting, "GET", "/next", nil))
	if response.Code != 503 || recorder.Snapshot().Active != 1 {
		t.Fatal("cancelled live handler released its admission")
	}
	if err := recorder.Gate().Set(true); err != nil {
		t.Fatal(err)
	}
	resume()
	<-exited
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/next", nil))
	if response.Code != 503 {
		t.Fatal("maintenance admitted new work")
	}
	if err := recorder.Gate().Set(false); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/next", nil))
	if response.Code != 204 || recorder.Snapshot().Active != 0 {
		t.Fatal("resumed server did not release ownership")
	}
}

func TestHTTPAbnormalHandlersReleaseObservationOwnership(t *testing.T) {
	for _, goexit := range []bool{false, true} {
		recorder := observationRecorder(t)
		handler := observationBoundary(t, DefaultServerConfig(), recorder, func(stdhttp.ResponseWriter, *stdhttp.Request) {
			if goexit {
				runtime.Goexit()
			}
			panic("private panic payload")
		})
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() {
				if recovered := recover(); recovered != nil && recovered != stdhttp.ErrAbortHandler {
					t.Error("panic payload escaped transport boundary")
				}
			}()
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("abnormal handler did not exit")
		}
		snapshot := recorder.Snapshot()
		if snapshot.Active != 0 || snapshot.Completed != 1 || snapshot.Recent[0].Result.Outcome != observability.Panicked {
			t.Fatal("abnormal handler stranded or misclassified its span", snapshot)
		}
	}
}

func TestObservedResponsePreservesNativeControlsAndStreaming(t *testing.T) {
	for _, mode := range []string{"direct", "transparent", "opaque"} {
		t.Run(mode, func(t *testing.T) {
			native := &etagControlTransport{header: make(stdhttp.Header)}
			var supplied stdhttp.ResponseWriter = native
			if mode == "transparent" {
				supplied = etagTransparentTransport{native}
			}
			if mode == "opaque" {
				supplied = etagOpaqueTransport{native}
			}
			capture := &observedResponse{native: supplied}
			writer := responseCapabilities(capture)
			_, flush := writer.(stdhttp.Flusher)
			_, hijack := writer.(stdhttp.Hijacker)
			_, push := writer.(stdhttp.Pusher)
			if direct := mode == "direct"; flush != direct || hijack != direct || push != direct {
				t.Fatal("changed native capability set")
			}
			if n, err := writer.(io.ReaderFrom).ReadFrom(io.LimitReader(strings.NewReader("body"), 4)); n != 4 || err != nil || native.body.String() != "body" || capture.status != 200 {
				t.Fatal("stream bypassed accounting", n, err)
			}
			controller := stdhttp.NewResponseController(writer)
			if err := controller.Flush(); mode == "opaque" {
				if !errors.Is(err, stdhttp.ErrNotSupported) {
					t.Fatal(err)
				}
			} else if err != nil || native.flushes != 1 {
				t.Fatal("flush failed", err)
			}
			_, _, err := controller.Hijack()
			if mode == "opaque" {
				if !errors.Is(err, stdhttp.ErrNotSupported) || capture.hijacked {
					t.Fatal(err)
				}
			} else if err != nil || !capture.hijacked || native.hijacks != 1 {
				t.Fatal("hijack was not retained", err)
			}
		})
	}
}

func TestMaintenanceReadExceptionsAreExactAndOwned(t *testing.T) {
	recorder := observationRecorder(t)
	if err := recorder.Gate().Set(true); err != nil {
		t.Fatal(err)
	}
	config := DefaultServerConfig()
	config.MaintenanceReadPaths = []string{"/operations"}
	handler := observationBoundary(t, config, recorder, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) })
	config.MaintenanceReadPaths[0] = "/changed"
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/operations", 204}, {"HEAD", "/operations", 204}, {"POST", "/operations", 503},
		{"GET", "/operations/extra", 503}, {"GET", "/changed", 503}, {"GET", "/%6fperations", 503},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Fatal("maintenance exception expanded or retained caller storage", test, response.Code)
		}
	}
}
