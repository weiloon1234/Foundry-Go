package http

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type StreamEventPayload struct {
	Name string `json:"name"`
}

func streamEventPayloadJSON() contract.JSON[StreamEventPayload] {
	return endpointDTO[StreamEventPayload](contract.Property{Name: "name", Type: "text", Required: true})
}

// serveEventRoute runs ServeEvents in a raw route and reports its result.
func serveEventRoute(t *testing.T, config EventStreamConfig, produce func(context.Context, *EventSink[StreamEventPayload]) error, w stdhttp.ResponseWriter, request *stdhttp.Request) error {
	t.Helper()
	var result error
	router, err := NewRouter(DefineRoute(RouteSpec{ID: "events", Method: GET, Access: Public}, StaticPath("/events")).HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) {
		result = ServeEvents(w, r, streamEventPayloadJSON(), config, produce)
	}))
	if err != nil {
		t.Fatal(err)
	}
	router.ServeHTTP(w, request)
	return result
}

func TestServeEventsWritesTypedEventsInOrder(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	result := serveEventRoute(t, DefaultEventStreamConfig(), func(ctx context.Context, sink *EventSink[StreamEventPayload]) error {
		for _, event := range []Event[StreamEventPayload]{
			{ID: "1", Name: "greeting", Data: StreamEventPayload{Name: "a\nb"}, Retry: 1500 * time.Millisecond},
			{Data: StreamEventPayload{Name: "plain"}},
		} {
			if err := sink.Send(ctx, event); err != nil {
				return err
			}
		}
		if err := sink.Send(ctx, Event[StreamEventPayload]{ID: "bad\nid"}); !errors.Is(err, fault.Invalid) {
			t.Error("multi-line ID accepted", err)
		}
		return nil
	}, response, httptest.NewRequest("GET", "/events", nil))
	if result != nil {
		t.Fatal(result)
	}
	want := "id: 1\nevent: greeting\nretry: 1500\ndata: {\"name\":\"a\\nb\"}\n\ndata: {\"name\":\"plain\"}\n\n"
	if response.Code != 200 || response.Body.String() != want || !response.Flushed {
		t.Fatalf("%d %q", response.Code, response.Body.String())
	}
	header := response.Header()
	if header.Get("Content-Type") != "text/event-stream; charset=utf-8" || header.Get("Cache-Control") != "no-store" || header.Get("Content-Length") != "" {
		t.Fatal("event stream headers", header)
	}
}

func TestServeEventsHeadRunsNoProducer(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	response := httptest.NewRecorder()
	result := serveEventRoute(t, DefaultEventStreamConfig(), func(context.Context, *EventSink[StreamEventPayload]) error {
		calls.Add(1)
		return nil
	}, response, httptest.NewRequest("HEAD", "/events", nil))
	if result != nil || calls.Load() != 0 || response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("HEAD event stream", result, calls.Load(), response.Code)
	}
}

func TestServeEventsHeartbeatAndCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		response := httptest.NewRecorder()
		exited := false
		done := make(chan error, 1)
		go func() {
			config := DefaultEventStreamConfig()
			config.Heartbeat = time.Second
			done <- serveEventRoute(t, config, func(ctx context.Context, sink *EventSink[StreamEventPayload]) error {
				<-ctx.Done()
				exited = true
				return ctx.Err()
			}, response, httptest.NewRequestWithContext(ctx, "GET", "/events", nil))
		}()
		time.Sleep(2500 * time.Millisecond)
		synctest.Wait()
		if strings.Count(response.Body.String(), ": keep-alive\n\n") != 2 {
			t.Fatalf("heartbeats %q", response.Body.String())
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) || !exited {
			t.Fatal("cancellation did not wait for the producer", err, exited)
		}
	})
}

type blockingEventWriter struct {
	header  stdhttp.Header
	entered chan struct{}
	release chan struct{}
	writes  atomic.Int32
	body    strings.Builder
}

func (w *blockingEventWriter) Header() stdhttp.Header { return w.header }
func (w *blockingEventWriter) WriteHeader(int)        {}
func (w *blockingEventWriter) Flush()                 {}
func (w *blockingEventWriter) Write(data []byte) (int, error) {
	if w.writes.Add(1) == 1 {
		close(w.entered)
		<-w.release
	}
	w.body.Write(data)
	return len(data), nil
}

// A full queue makes Send wait: a slow client slows its producer.
func TestServeEventsBoundedQueueAppliesBackpressure(t *testing.T) {
	t.Parallel()
	writer := &blockingEventWriter{header: make(stdhttp.Header), entered: make(chan struct{}), release: make(chan struct{})}
	config := DefaultEventStreamConfig()
	config.Queue = 1
	var blocked error
	result := serveEventRoute(t, config, func(ctx context.Context, sink *EventSink[StreamEventPayload]) error {
		for _, name := range []string{"first", "second"} {
			if err := sink.Send(ctx, Event[StreamEventPayload]{Data: StreamEventPayload{Name: name}}); err != nil {
				return err
			}
		}
		<-writer.entered
		wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		blocked = sink.Send(wait, Event[StreamEventPayload]{Data: StreamEventPayload{Name: "third"}})
		cancel()
		close(writer.release)
		return nil
	}, writer, httptest.NewRequest("GET", "/events", nil))
	if result != nil || !errors.Is(blocked, context.DeadlineExceeded) {
		t.Fatal("full queue did not block", result, blocked)
	}
	if body := writer.body.String(); !strings.Contains(body, "first") || !strings.Contains(body, "second") || strings.Contains(body, "third") {
		t.Fatalf("accepted events %q", body)
	}
}

func TestServeEventsFailuresEndTheStream(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"panic", "error", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			config := DefaultEventStreamConfig()
			config.Data.Bytes = 32
			sentinel := errors.New("producer failure")
			response := httptest.NewRecorder()
			var closed error
			result := serveEventRoute(t, config, func(ctx context.Context, sink *EventSink[StreamEventPayload]) error {
				switch mode {
				case "panic":
					panic("private producer payload")
				case "error":
					if err := sink.Send(ctx, Event[StreamEventPayload]{Data: StreamEventPayload{Name: "kept"}}); err != nil {
						return err
					}
					return sentinel
				}
				if err := sink.Send(ctx, Event[StreamEventPayload]{Data: StreamEventPayload{Name: strings.Repeat("x", 64)}}); err != nil {
					return err
				}
				<-ctx.Done()
				closed = sink.Send(context.Background(), Event[StreamEventPayload]{Data: StreamEventPayload{Name: "late"}})
				return nil
			}, response, httptest.NewRequest("GET", "/events", nil))
			switch mode {
			case "panic":
				if !errors.Is(result, InternalError) || strings.Contains(response.Body.String(), "private") {
					t.Fatal("producer panic", result)
				}
			case "error":
				if !errors.Is(result, sentinel) || !strings.Contains(response.Body.String(), "kept") {
					t.Fatal("producer error lost accepted events", result, response.Body.String())
				}
			case "oversized":
				if !errors.Is(result, InternalError) || response.Body.Len() != 0 || !errors.Is(closed, fault.Closed) {
					t.Fatal("oversized event", result, response.Body.String(), closed)
				}
			}
		})
	}
}

func TestEventStreamConfigValidation(t *testing.T) {
	t.Parallel()
	for _, config := range []EventStreamConfig{
		{Queue: 0, Data: DefaultEventStreamConfig().Data},
		{Queue: maxEventQueue + 1, Data: DefaultEventStreamConfig().Data},
		{Queue: 1, Heartbeat: time.Millisecond, Data: DefaultEventStreamConfig().Data},
		{Queue: 1},
	} {
		if err := config.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid event stream config accepted", config)
		}
	}
	if err := DefaultEventStreamConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func eventsEndpoint() Endpoint[NoPath, NoQuery, NoBody, Events[StreamEventPayload]] {
	return DefineEndpoint(DefineRoute(RouteSpec{ID: "members.events", Method: GET, Access: Public}, StaticPath("/events")), EmptyQuery(), EmptyBody(), EventStreamResponse(streamEventPayloadJSON()))
}

// A typed endpoint streams its handler's events after the handler succeeded,
// bounds data by EndpointLimits.Response and exposes Last-Event-ID for resume.
func TestEventStreamResponseStreamsTypedEvents(t *testing.T) {
	t.Parallel()
	var resumedFrom string
	router, err := NewRouter(eventsEndpoint().Handle(func(ctx context.Context, _ Input[NoPath, NoQuery, NoBody]) (Events[StreamEventPayload], error) {
		return EventsFrom(func(ctx context.Context, sink *EventSink[StreamEventPayload]) error {
			resumedFrom, _ = LastEventID(ctx)
			return sink.Send(ctx, Event[StreamEventPayload]{ID: "9", Data: StreamEventPayload{Name: "typed"}})
		}), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/events", nil)
	request.Header.Set("Last-Event-ID", "8")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != "id: 9\ndata: {\"name\":\"typed\"}\n\n" || resumedFrom != "8" || response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("%d %q %q", response.Code, response.Body.String(), resumedFrom)
	}
	invalid := httptest.NewRequest("GET", "/events", nil)
	invalid.Header["Last-Event-Id"] = []string{"1", "2"}
	router.ServeHTTP(httptest.NewRecorder(), invalid)
	if resumedFrom != "" {
		t.Fatal("repeated Last-Event-ID was accepted", resumedFrom)
	}
	info, err := eventsEndpoint().Description()
	if err != nil || info.Response == nil || info.Response.MediaType != EventStreamMediaType || info.Response.Schema.Root == "" || info.Status != 200 {
		t.Fatal("event stream metadata", info.Response, err)
	}
	if _, err := json.Marshal(EventsFrom(func(context.Context, *EventSink[StreamEventPayload]) error { return nil })); err == nil {
		t.Fatal("events were implicitly serialized")
	}
}

// A producer failure aborts the committed stream; a missing producer never
// commits a response.
func TestEventStreamResponseFailures(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"producer", "missing"} {
		t.Run(mode, func(t *testing.T) {
			router, err := NewRouter(eventsEndpoint().Handle(func(context.Context, Input[NoPath, NoQuery, NoBody]) (Events[StreamEventPayload], error) {
				if mode == "missing" {
					return Events[StreamEventPayload]{}, nil
				}
				return EventsFrom(func(context.Context, *EventSink[StreamEventPayload]) error {
					return errors.New("private producer failure")
				}), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			aborted := func() (aborted bool) {
				defer func() { aborted = recover() == stdhttp.ErrAbortHandler }()
				router.ServeHTTP(response, httptest.NewRequest("GET", "/events", nil))
				return false
			}()
			if mode == "producer" && !aborted || mode == "missing" && (aborted || response.Code != 500) {
				t.Fatal("event stream failure", mode, aborted, response.Code)
			}
		})
	}
}
