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

Streams use ordinary net/http buffering and are finite byte responses. They are
not a server-sent-event protocol or an infinite publisher. Storage-backed
adapters belong to milestone 11; realtime protocols belong to 14–15.

Focused canonical acceptance passed: HTTP and consumer races, compiler rejection
cases, actual gopls, fuzzing, bounded-copy benchmarks, vet and documentation checks.
Combined full regression including all three generation-freshness targets also
passed. [Static/SPA integration](http-assets.md) and [automatic ETags](http-etags.md)
also passed full regression. The [HTTP blueprint](../../blueprint/08-http-validation-and-responses.md)
records compression integration and final milestone acceptance.
