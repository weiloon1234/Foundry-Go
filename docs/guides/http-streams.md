# Typed stream responses

Use `StreamResponse` for finite representations whose source provides an
`io.ReadCloser`. Foundry owns transfer, byte limits and cleanup. Use
[downloads](http-downloads.md) when the source supports seeking and needs native
range or conditional requests. Streams do not supply cache validators and
respond to Range requests with the full representation and `Accept-Ranges: none`.
HTTP permits servers to ignore Range; see [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html#section-14.2).

The application owns `models.User` and any generated tables or identities used below.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `http` as `foundryhttp`, `model`. Standard-library aliases: `net/http` as `stdhttp`.

Declare the media and return a deferred source from the typed handler:

<!-- compiled-source: tests/fixtures/consumer/httpstreams/guide_recipes.go#report-stream-contracts -->
```go
// UserPath selects the user whose report is exported.
//
//foundry:path pattern=/users/{user}
type UserPath struct{ User model.ID[models.User] }

// ExportRequest carries the decoded user identity to the stream handler.
type ExportRequest = foundryhttp.Input[UserPath, foundryhttp.NoQuery, foundryhttp.NoBody]

// ReportSource authorizes access before returning a context-aware reader.
type ReportSource interface {
	Open(context.Context, model.ID[models.User]) (io.ReadCloser, error)
}
```

Inputs: `reports ReportSource`.

<!-- compiled-source: tests/fixtures/consumer/httpstreams/guide_recipes.go#http-streams-1 -->
```go
route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{
	ID: "reports.export", Method: foundryhttp.GET, Access: foundryhttp.Public,
}, UserPathDescriptor())
endpoint := foundryhttp.DefineEndpoint(
	route,
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
	foundryhttp.StreamResponse("text/csv; charset=utf-8"),
)
registration := endpoint.Handle(func(ctx context.Context, input ExportRequest) (foundryhttp.Stream, error) {
	return foundryhttp.StreamFrom(func(ctx context.Context) (foundryhttp.StreamContent, error) {
		body, err := reports.Open(ctx, input.Path.User)
		return foundryhttp.StreamContent{
			Body: body, Name: "report.csv", MediaType: "text/csv; charset=utf-8",
		}, err
	}), nil
})
```

`ExportRequest` aliases `foundryhttp.Input[UserPath, foundryhttp.NoQuery, foundryhttp.NoBody]`.
The recipe declares its own `UserPath`; its generated descriptor retains
`model.ID[models.User]`. `ReportSource` supplies the domain reader, and
`registerReportExport` assembles the typed route and handler. No framework-facing
writer or string model identifier appears in the handler.

`StreamContent.Length` is `value.Optional[int64]`. Leave it unset when length
is unknown; use `value.Set(int64(size))` for an exact known length, including
zero. Declared lengths must agree with EOF. A length over `EndpointLimits.Files.Bytes`
is rejected before transfer; unknown-length streams enforce the same byte cap
while reading. Transfers use one 32 KiB buffer and a one-byte boundary probe.
The final declared chunk is withheld until EOF is verified, so excess content
cannot masquerade as a completed exact-length transfer. Memory does not grow
with representation size; callback handling still allocates per chunk.

The source opens only after validation and a successful handler. Before it
returns, it owns its resources and producer goroutines. Once it returns a body,
Foundry closes that body exactly once, even if the source also returns an error.
HEAD opens metadata and closes without reading. Reads must terminate or honor
the supplied context; Foundry waits for active callbacks and never closes a
body concurrently with its read. Request-owned uploads remain valid until the
response has finished and its reader has closed.

Metadata and initial read failures use the shared JSON error contract. After
headers or body are sent, transfer errors abort the response; Foundry cannot
replace a partial file with JSON. A failed writer is never reused. Cleanup
failures are logged without replacing an otherwise completed response.

`WithName`, `WithMediaType` and `WithDisposition` return independent values and
reuse download filename/media validation. Media must be declared by the
response descriptor. A Stream cannot be implicitly JSON-serialized. Endpoint
metadata reports declared media and `Seekable: false`; it invents no JSON schema.

Streams use ordinary net/http buffering and are finite byte responses. For
incremental output, such as a long export or a log tail, `StreamFrom(source).Progressive()`
flushes each chunk once it has been accepted against the declared length and
byte limit; a flushed prefix can still be aborted by a later failure, never
completed early. Declare the route's `Timeout` (see
[typed endpoints](http-endpoints.md)) for streams that outlive the kernel
`RequestTimeout`. [Storage-backed adapters](storage.md) use owned readers and explicitly selected disks. [Realtime protocols](websocket.md) have separate connection and delivery contracts.

## Server-sent events

A raw route can publish typed server-sent events (`text/event-stream`) with
`ServeEvents`. Each event's data is encoded by a declared `contract.JSON[T]`,
so the payload type and wire checks match typed responses:

<!-- compiled-source: tests/fixtures/consumer/httpstreams/guide_recipes.go#order-event-contracts -->
```go
// OrderEvent carries one order transition to a subscriber.
//
//foundry:dto
type OrderEvent struct {
	Number string `json:"number"`
	State  string `json:"state"`
}
type orderUpdate struct {
	Cursor string
	Event  OrderEvent
}
type orderWatcher interface {
	// Watch closes its channel when ctx ends.
	Watch(context.Context, string) <-chan orderUpdate
}
```

Inputs: `orders orderWatcher`.

<!-- compiled-source: tests/fixtures/consumer/httpstreams/guide_recipes.go#http-streams-2 -->
```go
route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{
	ID: "orders.events", Method: foundryhttp.GET, Access: foundryhttp.Public,
}, foundryhttp.StaticPath("/orders/events"))
route = foundryhttp.ConfigureRoute(route, foundryhttp.Timeout(time.Hour))

registration := foundryhttp.HandleRaw(route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ foundryhttp.NoPath) {
	err := foundryhttp.ServeEvents(w, r, OrderEventJSON(), foundryhttp.DefaultEventStreamConfig(),
		func(ctx context.Context, sink *foundryhttp.EventSink[OrderEvent]) error {
			for update := range orders.Watch(ctx, r.Header.Get("Last-Event-ID")) {
				if err := sink.Send(ctx, foundryhttp.Event[OrderEvent]{ID: update.Cursor, Name: "order", Data: update.Event}); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		// The stream is committed; do not write a second response.
		return
	}
})
```

`ServeEvents` commits 200 with `Cache-Control: no-store`, then writes and flushes
each queued event and a `: keep-alive` comment after `Heartbeat` (default 15
seconds; zero disables it) without events. HEAD receives headers only. `Send`
validates single-line `ID`/`Name` text and a non-negative `Retry`, and waits while
the bounded queue (default 64) is full, so a slow client slows its producer. The
producer's context ends with the request (client disconnect, forced shutdown or
the route deadline) or a failed write; `ServeEvents` waits for the producer's
actual return and contains its panics and Goexit. Events accepted before a
producer returns normally are all written. An event whose data violates its
contract or `EventStreamConfig.Data` ends the stream without a partial event.
Compression and automatic ETags pass event streams through. Declare the route's
`Timeout` for the longest stream lifetime; the kernel `RequestTimeout` and
`WriteTimeout` otherwise end it. Raw-route event streams appear in route
inspection, not in generated clients.

### Typed event stream endpoints

Inputs: `orders orderWatcher`.

`EventStreamResponse(descriptor)` makes the same stream a typed endpoint
response, described in the manifest, OpenAPI (`text/event-stream` with
`x-foundry-event-data`) and the TypeScript client. The handler validates the
request as usual and returns `EventsFrom(producer)`; the stream starts only after
the handler succeeded:

<!-- compiled-source: tests/fixtures/consumer/httpstreams/guide_recipes.go#http-streams-3 -->
```go
route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "orders.events", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/orders/events"))

orderEvents := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
	foundryhttp.EventStreamResponse(OrderEventJSON()))
orderEvents = foundryhttp.ConfigureEndpoint(orderEvents, foundryhttp.Timeout(time.Hour))

registration := orderEvents.Handle(func(ctx context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Events[OrderEvent], error) {
	return foundryhttp.EventsFrom(func(ctx context.Context, sink *foundryhttp.EventSink[OrderEvent]) error {
		after, _ := foundryhttp.LastEventID(ctx) // Set when the client resumes.
		for update := range orders.Watch(ctx, after) {
			if err := sink.Send(ctx, foundryhttp.Event[OrderEvent]{ID: update.Cursor, Data: update.Event}); err != nil {
				return err
			}
		}
		return nil
	}), nil
})
```

Each typed event's data is bounded by `EndpointLimits.Response`, which clients receive.
Typed events use `DefaultEventStreamConfig()` for the queue and heartbeat.
Custom queue or heartbeat settings require a raw handler using `ServeEvents`.
Set its `EventStreamConfig.Data` limits explicitly; it does not use
`EndpointLimits.Response`. A raw route does not supply the typed endpoint's
generated stream metadata or TypeScript client contract.

`LastEventID(ctx)` returns a
single valid `Last-Event-ID` header (at most 1 KiB, no line breaks or NUL). A
typed stream whose handler completed after its deadline is a reported 503, never
an empty 200. A client disconnect or the route deadline ends the stream normally; a producer
error, a contained panic or an invalid event aborts the connection, which is
logged with a redacted diagnostic. The TypeScript client returns an
`EventStreamResult<T>`: iterate it once with `for await`, receiving frozen
`{ id?, name, data, retry? }` events whose data is decoded tolerantly, or call
`close()`; pass `Last-Event-ID` in the call headers to resume.

Focused canonical acceptance passed: HTTP and consumer races, compiler rejection
cases, actual gopls, fuzzing, bounded-copy benchmarks, vet and documentation checks.
Combined full regression including all three generation-freshness targets also
passed. [Static/SPA integration](http-assets.md) and [automatic ETags](http-etags.md)
also passed full regression.
