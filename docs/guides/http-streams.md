# Typed stream responses

Use `StreamResponse` for finite representations whose source provides an
`io.ReadCloser`. Foundry owns transfer, byte limits and cleanup. Use
[downloads](http-downloads.md) when the source supports seeking and needs native
range or conditional requests. Streams do not supply cache validators and
respond to Range requests with the full representation and `Accept-Ranges: none`.
HTTP permits servers to ignore Range; see [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html#section-14.2).

Declare the media and return a deferred source from the typed handler:

```go
endpoint := foundryhttp.DefineEndpoint(
    foundryhttp.DefineRoute(foundryhttp.RouteSpec{
        ID: "reports.export", Method: foundryhttp.GET, Access: foundryhttp.Public,
    }, httpkernel.UserPathDescriptor()),
    foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
    foundryhttp.StreamResponse("text/csv; charset=utf-8"),
)
registration := endpoint.Handle(func(ctx context.Context, input Request) (foundryhttp.Stream, error) {
    return foundryhttp.StreamFrom(func(ctx context.Context) (foundryhttp.StreamContent, error) {
        body, err := reports.Open(ctx, input.Path.User)
        return foundryhttp.StreamContent{
            Body: body, Name: "report.csv", MediaType: "text/csv; charset=utf-8",
        }, err
    }), nil
})
```

`Request` aliases `Input[httpkernel.UserPath, NoQuery, NoBody]`; the generated
path retains `model.ID[models.User]`. The complete
[independent consumer](../../tests/fixtures/consumer/httpstreams/streams.go)
shows the domain service, declaration and router assembly. No framework-facing
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
completed early. Declare the route's `WithTimeout` (see
[typed endpoints](http-endpoints.md)) for streams that outlive the kernel
`RequestTimeout`. Storage-backed adapters belong to milestone 11; realtime
protocols belong to 14–15.

## Server-sent events

A raw route can publish typed server-sent events (`text/event-stream`) with
`ServeEvents`. Each event's data is encoded by a declared `contract.JSON[T]`,
so the payload type and wire checks match typed responses:

```go
route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{
    ID: "orders.events", Method: foundryhttp.GET, Access: foundryhttp.Public,
}, foundryhttp.StaticPath("/orders/events")).WithTimeout(time.Hour)

registration := route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ foundryhttp.NoPath) {
    err := foundryhttp.ServeEvents(w, r, OrderEventJSON(), foundryhttp.DefaultEventStreamConfig(),
        func(ctx context.Context, sink *foundryhttp.EventSink[OrderEvent]) error {
            for update := range orders.Watch(ctx, r.Header.Get("Last-Event-ID")) {
                if err := sink.Send(ctx, foundryhttp.Event[OrderEvent]{ID: update.Cursor, Name: "order", Data: update.Event}); err != nil {
                    return err
                }
            }
            return nil
        })
    _ = err // The stream is committed; log a redacted diagnostic if needed.
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
`WithTimeout` for the longest stream lifetime; the kernel `RequestTimeout` and
`WriteTimeout` otherwise end it. Raw-route event streams appear in route
inspection, not in generated clients.

### Typed event stream endpoints

`EventStreamResponse(descriptor)` makes the same stream a typed endpoint
response, described in the manifest, OpenAPI (`text/event-stream` with
`x-foundry-event-data`) and the TypeScript client. The handler validates the
request as usual and returns `EventsFrom(producer)`; the stream starts only after
the handler succeeded:

```go
var OrderEvents = foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(),
    foundryhttp.EventStreamResponse(OrderEventJSON())).WithTimeout(time.Hour)

registration := OrderEvents.Handle(func(ctx context.Context, in Request) (foundryhttp.Events[OrderEvent], error) {
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

Each event's data is bounded by `EndpointLimits.Response`, which clients receive;
`Events.WithConfig` changes the queue and heartbeat. `LastEventID(ctx)` returns a
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
also passed full regression. The [HTTP blueprint](../../blueprint/08-http-validation-and-responses.md)
records compression integration and final milestone acceptance.
