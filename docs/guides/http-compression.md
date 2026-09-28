# HTTP response compression

Focused runtime, consumer, type-safety and editor acceptance passed, followed by
full canonical regression. See the [milestone completion review](../../blueprint/00-master-architecture-and-parity.md#milestone-08-completion-review).

Apply `Compression(DefaultCompressionConfig())` around a router or declare it in
a route's middleware chain. The [independent consumer](../../tests/fixtures/consumer/httpcompression/compression.go)
adds this wrapper to an existing typed endpoint; its service and DTOs stay ordinary
Go contracts.

```go
router, err := httpendpoints.Router(service)
if err != nil {
    return nil, err
}
return foundryhttp.ApplyMiddleware(
    router,
    foundryhttp.Compression(foundryhttp.DefaultCompressionConfig()),
)
```

The excerpt uses the consumer's imports and declarations. Runtime configuration
is copied during middleware assembly. Both gzip and Brotli reproduce Rust
Foundry's enabled compression capabilities. Gzip uses Go's standard library;
Brotli uses the stable writer API behind Foundry's private adapter.
[Go gzip](https://pkg.go.dev/compress/gzip),
[Brotli API](https://pkg.go.dev/github.com/andybalholm/brotli)

## Typed options and bounds

`CompressionConfig.Encoders` contains `CompressionEncoder` values, with server
preference in declaration order. Construct them with:

```go
foundryhttp.GzipCompression(foundryhttp.GzipFastest)
foundryhttp.BrotliCompression(foundryhttp.BrotliQuality(4), foundryhttp.BrotliWindow(20))
```

Gzip levels and Brotli quality/window values have distinct Go types. Invalid
levels, windows, duplicates or zero declarations fail middleware assembly.
The defaults prefer Brotli quality 4 with a 1 MiB window, then gzip's fastest
level. This does not use the upstream experimental WriterV2 API.

`MinBytes` defaults to 1024 and may be 0–65536. The middleware buffers at most
that many application bytes before selecting streaming compression. Large
writes are processed in chunks; `io.Copy` cannot bypass encoding through
`ReaderFrom`. Codec state and the caller's own payload allocation are additional
memory; this middleware does not buffer an entire response.

`MaxConcurrent` defaults to 32 and may be 1–1024. It bounds active encoders for
each assembled middleware instance. When saturated, requests use identity if
accepted; otherwise they receive the shared 503 response. Encoder requests do
not form an unbounded queue. Slots are released on normal return, write failure,
cancellation and panic. Interrupted streams are not finalized as successful
compressed representations.

## Negotiation and representation policy

`Accept-Encoding` parsing is bounded to 8 KiB, 16 lines and 64 list slots.
Quality weights use exact integer thousandths. Unknown valid coding names are
allowed, but unsupported encodings are not selected. Duplicate coding names,
malformed quality values and control bytes return 400. The highest accepted
encoding weight wins, with server order breaking ties. An explicitly higher
identity preference selects identity. Absent/empty fields select identity.
[HTTP encoding negotiation](https://httpwg.org/specs/rfc9110.html#field.accept-encoding)

Identity remains an acceptable fallback unless explicitly excluded by
`identity;q=0`, or `*;q=0` without an explicit identity entry. An otherwise safe
small response may be compressed below the size threshold when identity is
excluded. If no acceptable representation is available, Foundry sends the
shared 406 `not_acceptable` response and discards further application response
writes. This allows typed and native handlers to return without converting the
error response into an interrupted connection. Negotiation of a response does
not roll back domain work already performed by its handler.

The automatic policy compresses text, JSON, XML, JavaScript and media types with
`+json` or `+xml` suffixes. It excludes SSE, gRPC, already encoded content,
partial/range responses, responses with `Set-Cookie`, and responses declaring
`private`, `no-store` or `no-transform`. Incompressible formats such as PNG
remain unchanged. Existing encodings are sent unchanged only when acceptable.

The middleware merges `Vary: Accept-Encoding`. Encoding removes the original
`Content-Length`, weakens valid strong ETags and removes invalidated digest
headers/trailers. Other declared trailers retain their values. Header changes
after the first final `WriteHeader` cannot rewrite the response snapshot.

When using automatic validators, declare `ETags` before `Compression`:

```go
return foundryhttp.ApplyMiddleware(
    router,
    foundryhttp.ETags(foundryhttp.DefaultETagConfig()),
    foundryhttp.Compression(foundryhttp.DefaultCompressionConfig()),
)
```

The first middleware is outermost. This matches Rust Foundry's priority order
and lets the automatic ETag describe the selected encoded bytes. Identity,
gzip and Brotli representations receive distinct strong validators. A tag from
one representation must not validate another representation's bytes. Keeping
compression outside automatic ETags would instead transform an already hashed
body and weaken its validator; use the order above for automatic conditional
responses.

HEAD and 304 carry consistent metadata for the selected GET representation
without creating an encoder or response body. When determining the selected
variant requires an unseen GET payload, HEAD length/validator fields are omitted.
A conditional 304 retains an existing validator, weakened when necessary for a
potential transform, without inventing an encoded size or coding. This preserves
cache revalidation when native file handling omits body metadata.
[304 metadata requirements](https://www.rfc-editor.org/rfc/rfc9110.html#name-304-not-modified).
An identity HEAD retains a known identity length. Requests upgrading protocols
keep the original native writer; compression does not wrap WebSocket traffic.

## Streaming and native HTTP controls

`Flush` sends all encoded bytes produced so far; the stream becomes complete
only after normal handler return. An early flush below the size threshold uses
identity when allowed. ResponseController reaches flush, hijack and native
deadline controls through the wrapper without bypassing pending compression.
The wrapper advertises native Flusher/Hijacker/Pusher interfaces only when its
immediate underlying writer has them. Hijacking an active compressed stream
or a response with buffered content or unsent final headers returns
`http.ErrNotSupported`. Successful full-duplex activation before encoding stops
automatic transformation and preserves native response streaming thresholds;
encoding negotiation still applies. An active encoder cannot be converted back
to native full-duplex output and returns `http.ErrNotSupported`. An unsupported
native control does not discard buffered data.

Body writes and encoder finalization check the request context. The caller still
owns cancellation/deadlines for arbitrary blocking source readers; Foundry does
not abandon a reader or writer while it is executing. Failed or incomplete
compressed output aborts the HTTP response instead of appending a misleading
successful footer. A source error observed through `io.Copy` remains a failure
even if the handler ignores the returned error. Invalid source/writer byte
counts and repeated zero-progress reads are rejected. After a failure, the body
owner performs no further source I/O or body writes. This middleware does not
decompress incoming request bodies.

Acceptance includes real gzip/Brotli streaming before handler return, typed DTO
round trips, clean 406/503 responses, encoder saturation/release, HEAD/304,
prefix bounds, canceled writes, write failures, native controls, trailers,
compiler rejections and real-gopls lookup. Resource benchmarks report allocations
for small and large inputs; total allocated bytes are not a peak-memory claim.


The acceptance fixtures also compose compression with typed
Download/Stream responses and framework-owned assets/SPA fallback. They verify
payload decoding, source cleanup, HEAD, identity byte ranges, conditional files,
shared missing-resource errors and both Accept/Accept-Encoding variation.
The fixtures also cover automatic validators for actual encoded bytes,
cross-encoding preconditions, source failure retention, unsupported native controls
and a real full-duplex exchange through both wrappers. These checks passed under
race detection using the approved Brotli dependency.

Transfer failure diagnostics use safe framework messages and never format custom
reader/writer errors. The original cause remains available to the handler through
its I/O error, and failed transfers still abort before releasing encoder capacity.
