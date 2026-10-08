package http

import (
	"bytes"
	"context"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type classificationCallbackError struct {
	as          func(any) bool
	is          func(error) bool
	unwrap      func() error
	formatCalls atomic.Int32
}

func (e *classificationCallbackError) Error() string {
	e.formatCalls.Add(1)
	panic("private-error-format-detail")
}

func (e *classificationCallbackError) As(target any) bool {
	if e.as != nil {
		return e.as(target)
	}
	return false
}

func (e *classificationCallbackError) Is(target error) bool {
	if e.is != nil {
		return e.is(target)
	}
	return false
}

func (e *classificationCallbackError) Unwrap() error {
	if e.unwrap != nil {
		return e.unwrap()
	}
	return nil
}

func TestErrorClassificationOwnsPanicAndGoexit(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"as", "unwrap"} {
		for _, mode := range []string{"panic", "goexit"} {
			t.Run(method+"-"+mode, func(t *testing.T) {
				fail := func() {
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("private-classification-detail")
				}
				cause := new(classificationCallbackError)
				if method == "as" {
					cause.as = func(any) bool { fail(); return false }
				} else {
					cause.unwrap = func() error { fail(); return nil }
				}
				var logs bytes.Buffer
				request := withRequestLogger(httptest.NewRequest("GET", "/", nil), slog.New(slog.NewTextHandler(&logs, nil)))
				response := httptest.NewRecorder()
				if mode == "goexit" {
					// Classification runs on the caller's goroutine
					// (callback.Invoke): Goexit ends it before any write.
					if !exitsGoroutine(func() { _ = WriteError(response, request, cause) }) || response.Body.Len() != 0 {
						t.Fatal("classification Goexit was converted or wrote a response")
					}
					return
				}
				if err := WriteError(response, request, cause); err != nil {
					t.Fatal(err)
				}
				if failure := decodeFailure(t, response); failure.Code != InternalError || response.Code != 500 {
					t.Fatalf("unsafe classification: %+v", failure)
				}
				if cause.formatCalls.Load() != 0 || strings.Contains(response.Body.String()+logs.String(), "private") {
					t.Fatal("classification invoked or exposed private error formatting")
				}
				if !strings.Contains(logs.String(), "HTTP error classification failed") {
					t.Fatal("classification failure was not logged")
				}
			})
		}
	}
}

func TestErrorClassificationWaitsBeforeWriting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	cause := &classificationCallbackError{unwrap: func() error { close(entered); <-release; return Forbidden }}
	response := &classificationWriter{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan error, 1)
	go func() { done <- WriteError(response, httptest.NewRequestWithContext(ctx, "GET", "/", nil), cause) }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("classification abandoned its callback")
	default:
	}
	if response.writes.Load() != 0 {
		t.Fatal("error response committed while classification was pending")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if failure := decodeFailure(t, response.ResponseRecorder); failure.Code != Forbidden || response.Code != 403 {
		t.Fatalf("wrapped code lost: %+v", failure)
	}
	if cause.formatCalls.Load() != 0 {
		t.Fatal("ordinary wrapping formatted the error")
	}
}

type classificationWriter struct {
	*httptest.ResponseRecorder
	writes atomic.Int32
}

func (w *classificationWriter) WriteHeader(status int) {
	w.writes.Add(1)
	w.ResponseRecorder.WriteHeader(status)
}
func (w *classificationWriter) Write(body []byte) (int, error) {
	w.writes.Add(1)
	return w.ResponseRecorder.Write(body)
}

func TestErrorClassificationRetainsCustomAsAndOuterPrecedence(t *testing.T) {
	t.Parallel()
	cause := &classificationCallbackError{as: func(target any) bool {
		if slot, ok := target.(*classifiedError); ok {
			*slot = Conflict
			return true
		}
		return false
	}}
	for _, tc := range []struct {
		err  error
		code ErrorCode
	}{
		{cause, Conflict},
		{BadRequest.WithCause(cause), BadRequest},
	} {
		response := httptest.NewRecorder()
		if err := WriteError(response, httptest.NewRequest(stdhttp.MethodGet, "/", nil), tc.err); err != nil {
			t.Fatal(err)
		}
		if failure := decodeFailure(t, response); failure.Code != tc.code {
			t.Fatalf("classification precedence: %+v", failure)
		}
	}
	if cause.formatCalls.Load() != 0 {
		t.Fatal("custom As required unsafe Error formatting")
	}
}

func TestUnavailableRetryClassificationOwnsPanicAndGoexit(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"is", "unwrap"} {
		for _, mode := range []string{"panic", "goexit"} {
			t.Run(method+"-"+mode, func(t *testing.T) {
				var calls atomic.Int32
				fail := func() {
					calls.Add(1)
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("private-retry-detail")
				}
				cause := new(classificationCallbackError)
				if method == "is" {
					cause.is = func(error) bool { fail(); return false }
				} else {
					cause.unwrap = func() error { fail(); return nil }
				}
				// A departed client's failure is not reported. This isolates the
				// retry lookup from the earlier server-reporting inspections.
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				var logs bytes.Buffer
				request := withRequestLogger(httptest.NewRequestWithContext(ctx, "GET", "/", nil), slog.New(slog.NewTextHandler(&logs, nil)))
				response := &classificationWriter{ResponseRecorder: httptest.NewRecorder()}
				if mode == "goexit" {
					if !exitsGoroutine(func() { _ = WriteError(response, request, Unavailable.WithCause(cause)) }) || response.writes.Load() != 0 {
						t.Fatal("retry classification Goexit was converted or committed a response")
					}
				} else {
					if err := WriteError(response, request, Unavailable.WithCause(cause)); err != nil {
						t.Fatal(err)
					}
					if failure := decodeFailure(t, response.ResponseRecorder); failure.Code != Unavailable || response.Code != 503 || response.Header().Get("Retry-After") != "" {
						t.Fatalf("retry failure changed the selected response: %+v", failure)
					}
					if !strings.Contains(logs.String(), "HTTP overload retry classification failed") {
						t.Fatal("retry classification failure was not logged")
					}
				}
				if calls.Load() != 1 || cause.formatCalls.Load() != 0 || strings.Contains(response.Body.String()+logs.String(), "private") {
					t.Fatal("retry inspection ran outside its owner or exposed private formatting")
				}
			})
		}
	}
}

func TestUnavailableRetryPanicPermitsTheNextRequest(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"is", "unwrap"} {
		t.Run(method, func(t *testing.T) {
			cause := new(classificationCallbackError)
			if method == "is" {
				cause.is = func(error) bool { panic("private-retry-detail") }
			} else {
				cause.unwrap = func() error { panic("private-retry-detail") }
			}
			fail := true
			router, err := NewRouter(errorEndpoint("errors.retry", "/retry").Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (NoContent, error) {
				if fail {
					return NoContent{}, Unavailable.WithCause(cause)
				}
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/retry", nil))
			if response.Code != 503 || decodeFailure(t, response).Code != Unavailable || response.Header().Get("Retry-After") != "" {
				t.Fatal("retry panic escaped the route's selected response")
			}
			if cause.formatCalls.Load() != 0 || strings.Contains(response.Body.String(), "private") {
				t.Fatal("retry panic exposed private error formatting")
			}
			fail = false
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/retry", nil))
			if response.Code != 204 {
				t.Fatal("retry failure prevented the next request")
			}
		})
	}
}

func TestUnavailableRetryRetainsCustomIsAndExistingHint(t *testing.T) {
	t.Parallel()
	cause := &classificationCallbackError{is: func(target error) bool { return target == fault.Overloaded }}
	for _, retry := range []string{"", "30"} {
		response := httptest.NewRecorder()
		if retry != "" {
			response.Header().Set("Retry-After", retry)
		}
		if err := WriteError(response, httptest.NewRequest("GET", "/", nil), Unavailable.WithCause(cause)); err != nil {
			t.Fatal(err)
		}
		want := retry
		if want == "" {
			want = "1"
		}
		if response.Code != 503 || decodeFailure(t, response).Code != Unavailable || response.Header().Get("Retry-After") != want {
			t.Fatal("contained retry inspection lost a valid overload or existing hint")
		}
	}
}
