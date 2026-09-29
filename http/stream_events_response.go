package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// EventStreamMediaType is the media type of server-sent event responses.
const EventStreamMediaType = "text/event-stream"

// Events is a typed endpoint's server-sent event stream. Build it with
// EventsFrom; its producer runs after the handler succeeded, while the response
// streams. Copies share the producer but run independently for each request.
type Events[T any] struct {
	produce func(context.Context, *EventSink[T]) error
	config  EventStreamConfig
}

// EventsFrom declares the stream's producer; see ServeEvents for its contract.
func EventsFrom[T any](produce func(context.Context, *EventSink[T]) error) Events[T] {
	return Events[T]{produce: produce, config: DefaultEventStreamConfig()}
}

// WithConfig replaces the queue and heartbeat. A typed endpoint bounds each
// event's data by its EndpointLimits.Response, which clients also receive, so
// config.Data is ignored here.
func (e Events[T]) WithConfig(config EventStreamConfig) Events[T] {
	e.config = config
	return e
}

// MarshalJSON rejects implicit serialization; events require their response contract.
func (Events[T]) MarshalJSON() ([]byte, error) {
	return nil, fault.New(fault.Invalid, "events require an event stream response contract")
}

// EventStreamResponse declares a server-sent event (text/event-stream) response
// whose event data is typed by descriptor. The handler returns Events built by
// EventsFrom; the stream starts only after it succeeded, then behaves as
// ServeEvents (flush per event, heartbeats, bounded queue, owned producer). Each
// event's data is bounded by EndpointLimits.Response. Declare the route's
// WithTimeout for the longest stream lifetime. A client disconnect or the route
// deadline ends the stream normally; a producer failure or an invalid event
// aborts the connection. Metadata, OpenAPI and the TypeScript client describe
// the stream and its data type; the client iterates the decoded events.
func EventStreamResponse[T any](descriptor contract.JSON[T]) Response[Events[T]] {
	return Response[Events[T]]{kind: payloadEvents, status: 200, events: eventsDescriptor[T]{descriptor: descriptor}}
}

// eventResponse is a typed event stream contract behind a Response[R].
type eventResponse[R any] interface {
	Validate() error
	Description() (contract.Schema, error)
	prepare(R, contract.JSONLimits) (func(stdhttp.ResponseWriter, *stdhttp.Request) error, error)
}

type eventsDescriptor[T any] struct{ descriptor contract.JSON[T] }

func (d eventsDescriptor[T]) Validate() error { return d.descriptor.Validate() }
func (d eventsDescriptor[T]) Description() (contract.Schema, error) {
	return d.descriptor.Description()
}

func (d eventsDescriptor[T]) prepare(events Events[T], limits contract.JSONLimits) (func(stdhttp.ResponseWriter, *stdhttp.Request) error, error) {
	if events.produce == nil {
		return nil, fault.New(fault.Invalid, "event stream has no producer")
	}
	config := events.config
	config.Data = limits
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) error {
		if err := r.Context().Err(); err != nil {
			// The request ended before the stream could start. Report it
			// instead of committing an empty 200 event stream.
			writeRoutingError(w, r, Unavailable.WithCause(err))
			return nil
		}
		err := ServeEvents(w, r, d.descriptor, config, events.produce)
		if err != nil && r.Context().Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			// The client left or the route deadline ended the stream.
			return nil
		}
		return err
	}, nil
}

type lastEventIDKey struct{}

// LastEventID returns the Last-Event-ID a reconnecting client sent to a typed
// event stream endpoint, so its producer can resume after that event. A missing,
// repeated or invalid header (over 1 KiB, invalid UTF-8, NUL or line breaks) is
// absent.
func LastEventID(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(lastEventIDKey{}).(string)
	return id, ok
}

// withLastEventID records a valid Last-Event-ID header for LastEventID.
func withLastEventID(r *stdhttp.Request) *stdhttp.Request {
	values := r.Header.Values("Last-Event-ID")
	if len(values) != 1 || values[0] == "" || len(values[0]) > maxEventFieldBytes || !utf8.ValidString(values[0]) || strings.ContainsAny(values[0], "\r\n\x00") {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), lastEventIDKey{}, values[0]))
}
