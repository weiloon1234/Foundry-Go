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
)

type classificationCallbackError struct {
	as          func(any) bool
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
