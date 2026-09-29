package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func deadlineBoundary(owner *handlerLifetime, handler stdhttp.Handler, timeout time.Duration) stdhttp.Handler {
	config := DefaultServerConfig()
	config.RequestTimeout = timeout
	return owner.wrap(handler, slog.New(slog.NewTextHandler(io.Discard, nil)), config)
}

func TestRequestDeadlinePreservesEarlierParentAndCancelsOnReturn(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	parent = context.WithValue(parent, applicationValue{}, "kept")
	deadline, _ := parent.Deadline()
	var received context.Context
	handler := deadlineBoundary(newHandlerLifetime(), stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		received = r.Context()
		actual, ok := received.Deadline()
		if !ok || !actual.Equal(deadline) || received.Value(applicationValue{}) != "kept" || RequestID(received) == "" {
			t.Error("request deadline lost parent constraints or attribution")
		}
		w.WriteHeader(204)
	}), time.Minute)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(parent, "GET", "/", nil))
	if received == nil || !errors.Is(received.Err(), context.Canceled) {
		t.Fatal("completed request retained live deadline resources")
	}
	if parent.Err() != nil {
		t.Fatal("request completion canceled the parent")
	}
}

func TestRequestDeadlineRetainsHandlerOwnershipUntilExit(t *testing.T) {
	t.Parallel()
	owner := newHandlerLifetime()
	expired, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := deadlineBoundary(owner, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		<-r.Context().Done()
		if !errors.Is(r.Context().Err(), context.DeadlineExceeded) {
			t.Error("request timeout did not expire")
		}
		close(expired)
		<-release
		if err := WriteError(w, r, RequestTimeout.WithCause(r.Context().Err())); err != nil {
			t.Error(err)
		}
	}), 10*time.Millisecond)
	w := httptest.NewRecorder()
	go func() { defer close(done); handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil)) }()
	select {
	case <-expired:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("request deadline was not applied")
	}
	owner.seal()
	select {
	case <-done:
		t.Error("deadline abandoned the handler")
	default:
	}
	select {
	case <-owner.done:
		t.Error("deadline released application ownership early")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("released handler did not finish")
	}
	select {
	case <-owner.done:
	default:
		t.Fatal("finished handler retained application ownership")
	}
	if w.Code != 408 || decodeFailure(t, w).Code != RequestTimeout {
		t.Fatal("cooperative timeout response was lost")
	}
}

// A handler's completed success is authoritative: a deadline that expired
// before it returned never replaces the success with a timeout.
func TestTypedEndpointPublishesSuccessCompletedAfterKernelDeadline(t *testing.T) {
	t.Parallel()
	e := DefineEndpoint(DefineRoute(RouteSpec{ID: "timeout", Method: GET, Access: Public}, StaticPath("/")), EmptyQuery(), EmptyBody(), JSONResponse(200, endpointReplyJSON()))
	router, err := NewRouter(e.Handle(func(ctx context.Context, _ Input[NoPath, NoQuery, NoBody]) (EndpointReply, error) {
		<-ctx.Done()
		return EndpointReply{Name: "expired success"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	deadlineBoundary(newHandlerLifetime(), router, 10*time.Millisecond).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "expired success") {
		t.Fatal("typed endpoint replaced its completed success after the deadline", w.Code, w.Body.String())
	}
}
