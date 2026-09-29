package http

import (
	"bytes"
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

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// deadlineKernel wraps a router with global Compression and ETags inside a
// kernel whose RequestTimeout is short, and records the kernel log.
func deadlineKernel(t *testing.T, requestTimeout time.Duration, registrations ...RouteRegistration) (stdhttp.Handler, *lockedBuffer) {
	t.Helper()
	router, err := NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, Compression(DefaultCompressionConfig()), ETags(DefaultETagConfig()))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultServerConfig()
	config.RequestTimeout = requestTimeout
	logs := &lockedBuffer{}
	return newHandlerLifetime().wrap(handler, slog.New(slog.NewTextHandler(logs, nil)), config), logs
}

type lockedBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.data.String() }

// Global wrappers observe the matched route's own budget: a WithTimeout event
// stream and a progressive stream outlive the kernel RequestTimeout, and a
// completed success is delivered after the deadline.
func TestGlobalWrappersHonorRouteBudgetAndCompletedResponses(t *testing.T) {
	t.Parallel()
	events := DefineEndpoint(DefineRoute(RouteSpec{ID: "events", Method: GET, Access: Public}, StaticPath("/events")), EmptyQuery(), EmptyBody(), EventStreamResponse(streamEventPayloadJSON())).WithTimeout(5 * time.Second)
	export := DefineEndpoint(DefineRoute(RouteSpec{ID: "export", Method: GET, Access: Public}, StaticPath("/export")), EmptyQuery(), EmptyBody(), StreamResponse("text/plain; charset=utf-8")).WithTimeout(5 * time.Second)
	late := DefineEndpoint(DefineRoute(RouteSpec{ID: "late", Method: GET, Access: Public}, StaticPath("/late")), EmptyQuery(), EmptyBody(), JSONResponse(200, streamEventPayloadJSON()))
	failed := DefineEndpoint(DefineRoute(RouteSpec{ID: "failed", Method: GET, Access: Public}, StaticPath("/failed")), EmptyQuery(), EmptyBody(), JSONResponse(200, streamEventPayloadJSON()))
	kernel, _ := deadlineKernel(t, 50*time.Millisecond,
		events.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (Events[StreamEventPayload], error) {
			return EventsFrom(func(ctx context.Context, sink *EventSink[StreamEventPayload]) error {
				for _, name := range []string{"first", "second", "third"} {
					if err := sink.Send(ctx, Event[StreamEventPayload]{Data: StreamEventPayload{Name: name}}); err != nil {
						return err
					}
					time.Sleep(60 * time.Millisecond)
				}
				return nil
			}), nil
		}),
		export.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (Stream, error) {
			chunks := []string{strings.Repeat("a", 2048), strings.Repeat("b", 2048), strings.Repeat("c", 2048)}
			return StreamFrom(func(context.Context) (StreamContent, error) {
				index := 0
				return StreamContent{MediaType: "text/plain; charset=utf-8", Body: fileReaderCallbacks{read: func(p []byte) (int, error) {
					if index == len(chunks) {
						return 0, io.EOF
					}
					time.Sleep(60 * time.Millisecond)
					n := copy(p, chunks[index])
					index++
					return n, nil
				}, close: func() error { return nil }}}, nil
			}).Progressive(), nil
		}),
		failed.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (StreamEventPayload, error) {
			time.Sleep(80 * time.Millisecond) // A returned error is published as returned.
			return StreamEventPayload{}, Conflict
		}),
		late.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (StreamEventPayload, error) {
			time.Sleep(80 * time.Millisecond) // Past the kernel deadline, then succeed.
			return StreamEventPayload{Name: strings.Repeat("completed ", 200)}, nil
		}),
	)
	for _, tc := range []struct {
		path   string
		want   func(string) bool
		status int
	}{
		{"/failed", func(body string) bool { return strings.Contains(body, `"error_code":"conflict"`) }, 409},
		{"/events", func(body string) bool { return strings.Count(body, "data: ") == 3 && strings.Contains(body, "third") }, 200},
		{"/export", func(body string) bool { return len(body) == 3*2048 && strings.HasSuffix(body, "c") }, 200},
		{"/late", func(body string) bool { return strings.Contains(body, "completed") }, 200},
	} {
		request := httptest.NewRequest("GET", tc.path, nil)
		response := httptest.NewRecorder()
		kernel.ServeHTTP(response, request)
		if response.Code != tc.status || !tc.want(response.Body.String()) {
			t.Fatalf("%s: %d %q", tc.path, response.Code, response.Body.String())
		}
	}
}

// A typed event stream whose handler completed after the deadline is a
// reported 503, never an empty 200.
func TestLateEventStreamIsReportedNotEmpty(t *testing.T) {
	t.Parallel()
	events := DefineEndpoint(DefineRoute(RouteSpec{ID: "events", Method: GET, Access: Public}, StaticPath("/events")), EmptyQuery(), EmptyBody(), EventStreamResponse(streamEventPayloadJSON()))
	kernel, logs := deadlineKernel(t, 20*time.Millisecond, events.Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (Events[StreamEventPayload], error) {
		time.Sleep(50 * time.Millisecond)
		return EventsFrom(func(context.Context, *EventSink[StreamEventPayload]) error { return nil }), nil
	}))
	response := httptest.NewRecorder()
	kernel.ServeHTTP(response, httptest.NewRequest("GET", "/events", nil))
	if response.Code != 503 || strings.HasPrefix(response.Header().Get("Content-Type"), EventStreamMediaType) || !strings.Contains(logs.String(), "HTTP request failed") {
		t.Fatalf("late event stream: %d %q %s", response.Code, response.Header().Get("Content-Type"), logs.String())
	}
}

// Work after an expired deadline keeps real cancellation: a client that
// leaves (or already left) ends it; the deadline alone does not.
func TestCompletedContextDetachesOnlyTheDeadline(t *testing.T) {
	t.Parallel()
	for _, departed := range []bool{false, true} {
		parent, leave := context.WithCancel(t.Context())
		if departed {
			leave()
		}
		scope := &requestScope{}
		scope.kernel = requestBudget{parent: parent}
		scope.budget = &scope.kernel
		expired, stop := context.WithDeadline(context.WithValue(parent, requestScopeKey{}, scope), time.Now().Add(-time.Second))
		completed, release := completedContext(expired)
		if departed {
			if !errors.Is(completed.Err(), context.Canceled) {
				t.Fatal("a departed client did not end detached work", completed.Err())
			}
		} else {
			if completed.Err() != nil {
				t.Fatal("the expired deadline still ended detached work")
			}
			leave()
			select {
			case <-completed.Done():
			case <-time.After(time.Second):
				t.Fatal("client disconnect did not end detached work")
			}
		}
		release()
		stop()
		leave()
	}
	live, release := completedContext(t.Context())
	if live != t.Context() {
		t.Fatal("an unexpired context was replaced")
	}
	release()
}

// Admission rejections are counted, not described and logged as failures;
// real server failures stay reported.
func TestAdmissionRejectionsAreNotReportedAsFailures(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	router, err := NewRouter(
		timeoutRoute("held", 0, func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			entered <- struct{}{}
			<-hold
			w.WriteHeader(204)
		}),
		timeoutRoute("broken", 0, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			_ = WriteError(w, r, InternalError.WithCause(errors.New("private failure")))
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultServerConfig()
	config.MaxConcurrentRequests = 1
	logs := &lockedBuffer{}
	kernel := newHandlerLifetime().wrap(router, slog.New(slog.NewTextHandler(logs, nil)), config)
	done := make(chan struct{})
	go func() {
		defer close(done)
		kernel.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/held", nil))
	}()
	<-entered
	waiting, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	rejected := httptest.NewRecorder()
	kernel.ServeHTTP(rejected, httptest.NewRequestWithContext(waiting, "GET", "/held", nil))
	cancel()
	close(hold)
	<-done
	if rejected.Code != 503 || strings.Contains(logs.String(), "HTTP request failed") {
		t.Fatalf("capacity rejection: %d %s", rejected.Code, logs.String())
	}
	for _, err := range []error{Unavailable, Unavailable.WithCause(fault.New(fault.Overloaded, "full")), Unavailable.WithCause(fault.New(fault.Closed, "closing"))} {
		if !admissionRejection(err) {
			t.Fatal("rejection reported as a failure", err)
		}
	}
	if admissionRejection(Unavailable.WithCause(context.DeadlineExceeded)) {
		t.Fatal("a deadline failure was treated as a rejection")
	}
	broken := httptest.NewRecorder()
	kernel.ServeHTTP(broken, httptest.NewRequest("GET", "/broken", nil))
	if broken.Code != 500 || !strings.Contains(logs.String(), "HTTP request failed") || strings.Contains(logs.String(), "private failure") {
		t.Fatalf("server failure: %d %s", broken.Code, logs.String())
	}
}

type GoexitEventPayload struct {
	Name string `json:"name"`
}

// MarshalJSON exits its goroutine, as a hostile codec may.
func (GoexitEventPayload) MarshalJSON() ([]byte, error) {
	runtime.Goexit()
	return nil, nil
}

// A writer that exits abnormally still stops the sink and waits for the
// producer before the request is released.
func TestServeEventsReleasesOwnershipOnGoexit(t *testing.T) {
	t.Parallel()
	descriptor := endpointDTO[GoexitEventPayload](contract.Property{Name: "name", Type: "text", Required: true})
	var producerReturned bool
	exited := make(chan bool, 1)
	go func() {
		completed := false
		defer func() { exited <- completed }()
		request := httptest.NewRequest("GET", "/events", nil)
		_ = ServeEvents(httptest.NewRecorder(), request, descriptor, DefaultEventStreamConfig(), func(ctx context.Context, sink *EventSink[GoexitEventPayload]) error {
			defer func() { producerReturned = true }()
			if err := sink.Send(ctx, Event[GoexitEventPayload]{Data: GoexitEventPayload{Name: "x"}}); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		})
		completed = true
	}()
	if <-exited {
		t.Fatal("codec Goexit was converted into a return")
	}
	if !producerReturned {
		t.Fatal("request released while the producer was still running")
	}
}
