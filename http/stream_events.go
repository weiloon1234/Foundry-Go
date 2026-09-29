package http

import (
	"bytes"
	"context"
	"io"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Event is one server-sent event. Data is encoded by the stream's declared JSON
// contract. ID and Name are optional single-line text (at most 1 KiB, no NUL);
// an empty Name is the client's default "message" event. A positive Retry
// advises the client's reconnection delay, sent in whole milliseconds.
type Event[T any] struct {
	ID    string
	Name  string
	Data  T
	Retry time.Duration
}

const (
	maxEventQueue      = 4096
	maxEventFieldBytes = 1024
	maxEventRetry      = 24 * time.Hour
	minEventHeartbeat  = time.Second
)

func (e Event[T]) validate() error {
	for _, field := range []string{e.ID, e.Name} {
		if len(field) > maxEventFieldBytes || !utf8.ValidString(field) || strings.ContainsAny(field, "\r\n\x00") {
			return fault.New(fault.Invalid, "event ID and name must be bounded single-line text")
		}
	}
	if e.Retry < 0 || e.Retry > maxEventRetry {
		return fault.New(fault.Invalid, "event retry must be between zero and 24 hours")
	}
	return nil
}

// EventStreamConfig bounds one text/event-stream response.
type EventStreamConfig struct {
	// Queue bounds events accepted by Send but not yet written (1–4096). Send
	// waits while the queue is full, so a slow client slows its producer.
	Queue int
	// Heartbeat writes a comment line when no event was written for this long,
	// so idle proxies keep the connection open. Zero disables it; otherwise it
	// must be at least one second.
	Heartbeat time.Duration
	// Data bounds each event's encoded payload.
	Data contract.JSONLimits
}

// DefaultEventStreamConfig returns a 64-event queue, a 15-second heartbeat and
// 64 KiB event payloads.
func DefaultEventStreamConfig() EventStreamConfig {
	return EventStreamConfig{Queue: 64, Heartbeat: 15 * time.Second, Data: contract.JSONLimits{Bytes: 64 << 10, Depth: 32, Nodes: 4096, Steps: 16384, Issues: 16}}
}

func (c EventStreamConfig) Validate() error {
	if c.Queue <= 0 || c.Queue > maxEventQueue || c.Heartbeat < 0 || c.Heartbeat > 0 && c.Heartbeat < minEventHeartbeat {
		return fault.New(fault.Invalid, "event stream requires a queue of 1–4096 and a heartbeat of zero or at least one second")
	}
	return c.Data.Validate()
}

// EventSink accepts events for one stream. Send is safe for concurrent use by
// the producer and goroutines it owns, until the producer returns.
type EventSink[T any] struct {
	queue   chan Event[T]
	stopped chan struct{}
}

// Send validates one event and queues it in order, waiting while the queue is
// full. It returns ctx's error if ctx ends first, fault.Closed once the stream
// has ended (client disconnect, deadline or write failure), and fault.Invalid
// for an invalid ID, Name or Retry. Events still queued when a stream ends
// early are discarded; after a producer returns normally they are all written.
func (s *EventSink[T]) Send(ctx context.Context, event Event[T]) error {
	if err := event.validate(); err != nil {
		return err
	}
	select {
	case <-s.stopped:
		return fault.New(fault.Closed, "event stream ended")
	default:
	}
	select {
	case s.queue <- event:
		return nil
	case <-s.stopped:
		return fault.New(fault.Closed, "event stream ended")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ServeEvents writes a server-sent event (text/event-stream) response from a
// raw route handler. It commits 200 with Cache-Control: no-store, then runs
// produce and writes each queued event as soon as possible, flushing per event
// and writing heartbeat comments while idle. HEAD receives the headers only and
// never runs produce. Compression and ETag middleware pass event streams through.
//
// produce receives a context that ends when the request ends (client
// disconnect, forced shutdown or the route deadline) or the stream fails, and
// must return promptly then; ServeEvents waits for its actual return, including
// after cancellation, and contains its panics and Goexit. Declare the route's
// WithTimeout for the stream's longest lifetime: the kernel RequestTimeout and
// WriteTimeout otherwise end it. The client resumes with the Last-Event-ID
// request header, which the handler reads from the request.
//
// It returns nil after produce returned nil and every accepted event was
// written. Otherwise it returns the first failure: produce's own error (after
// writing the events it already accepted), a contained producer panic as an
// internal error, an invalid or oversized event payload (the stream ends; no
// partial event is written), the request context's error, or a write failure.
// The response is committed by then, so the handler cannot send an error body.
func ServeEvents[T any](w stdhttp.ResponseWriter, r *stdhttp.Request, descriptor contract.JSON[T], config EventStreamConfig, produce func(context.Context, *EventSink[T]) error) error {
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if produce == nil {
		return fault.New(fault.Invalid, "event stream requires a producer")
	}
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		return err
	}
	header := w.Header()
	clearResponseRepresentation(header)
	header.Set("Content-Type", EventStreamMediaType+"; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	// Reverse proxies such as nginx otherwise buffer the stream.
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(stdhttp.StatusOK)
	if r.Method == stdhttp.MethodHead {
		return nil
	}
	stream := eventWriter[T]{w: w, controller: stdhttp.NewResponseController(w), descriptor: descriptor, limits: config.Data}
	if err := stream.flush(); err != nil {
		return err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sink := &EventSink[T]{queue: make(chan Event[T], config.Queue), stopped: make(chan struct{})}
	produced := make(chan error, 1)
	go func() {
		var returned error
		if failure := callback.Isolated("HTTP event producer", func() error { returned = produce(streamCtx, sink); return nil }); failure != nil {
			returned = InternalError.WithCause(failure)
		}
		produced <- returned
	}()
	finished := false
	// Deferred, so a writer that exits abnormally (for example runtime.Goexit
	// in an event's codec) still stops the sink and waits for the producer.
	defer func() {
		close(sink.stopped)
		cancel()
		if !finished {
			// Keep ownership until the producer actually returns.
			<-produced
		}
	}()
	var result error
	finished, result = stream.run(ctx, sink, produced, config.Heartbeat)
	return result
}

type eventWriter[T any] struct {
	w          stdhttp.ResponseWriter
	controller *stdhttp.ResponseController
	descriptor contract.JSON[T]
	limits     contract.JSONLimits
	buffer     bytes.Buffer
}

// run writes events until the producer returns (finished) or the request or a
// write ends the stream first.
func (s *eventWriter[T]) run(ctx context.Context, sink *EventSink[T], produced <-chan error, heartbeat time.Duration) (bool, error) {
	var beats <-chan time.Time
	var timer *time.Timer
	if heartbeat > 0 {
		timer = time.NewTimer(heartbeat)
		defer timer.Stop()
		beats = timer.C
	}
	idle := func() {
		if timer != nil {
			timer.Reset(heartbeat)
		}
	}
	for {
		select {
		case event := <-sink.queue:
			if err := s.event(ctx, event); err != nil {
				return false, err
			}
			idle()
		case <-beats:
			if err := s.write([]byte(": keep-alive\n\n")); err != nil {
				return false, err
			}
			idle()
		case returned := <-produced:
			// Events accepted before the producer returned are still written.
			for {
				select {
				case event := <-sink.queue:
					if err := s.event(ctx, event); err != nil {
						return true, err
					}
					continue
				default:
				}
				return true, returned
			}
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func (s *eventWriter[T]) event(ctx context.Context, event Event[T]) error {
	data, err := s.descriptor.Encode(ctx, event.Data, s.limits)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		return InternalError.WithCause(fault.Wrap(fault.Internal, "event data failed its contract or EventStreamConfig.Data bounds", err))
	}
	s.buffer.Reset()
	if event.ID != "" {
		s.buffer.WriteString("id: ")
		s.buffer.WriteString(event.ID)
		s.buffer.WriteByte('\n')
	}
	if event.Name != "" {
		s.buffer.WriteString("event: ")
		s.buffer.WriteString(event.Name)
		s.buffer.WriteByte('\n')
	}
	if event.Retry > 0 {
		s.buffer.WriteString("retry: ")
		s.buffer.WriteString(strconv.FormatInt(event.Retry.Milliseconds(), 10))
		s.buffer.WriteByte('\n')
	}
	// Valid JSON contains CR or LF only as insignificant whitespace, and SSE
	// treats either as a line end: drop CR and split lines into data fields,
	// which the client rejoins with LF.
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		s.buffer.WriteString("data: ")
		for _, c := range line {
			if c != '\r' {
				s.buffer.WriteByte(c)
			}
		}
		s.buffer.WriteByte('\n')
	}
	s.buffer.WriteByte('\n')
	return s.write(s.buffer.Bytes())
}

func (s *eventWriter[T]) write(data []byte) error {
	written, err := s.w.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fault.Wrap(fault.Internal, "event stream could not be written", err)
	}
	return s.flush()
}

func (s *eventWriter[T]) flush() error {
	if err := s.controller.Flush(); err != nil {
		return fault.Wrap(fault.Internal, "event stream could not be flushed", err)
	}
	return nil
}
