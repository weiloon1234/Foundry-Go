# Automatic ETags

Use `ETags(DefaultETagConfig())` through ordinary middleware assembly:

```go
config := foundryhttp.DefaultETagConfig()
handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.ETags(config))
```

The [independent consumer](../../tests/fixtures/consumer/httpetags/etags.go)
wraps a typed endpoint. Its existing model presenter explicitly calls getters
and returns a generated DTO contract. The validator describes the resulting
response bytes; it does not read, serialize or alter stored model fields.

A complete, bounded successful GET response receives a quoted SHA-256 validator.
Matching `If-None-Match` conditions can return 304 with no body. Weak tags,
lists, wildcards and precondition ordering use Go's native conditional handling.
Failed `If-Match` conditions use the shared 412 error response. Other successful
statuses retain their original status when the response is sent.

The handler still runs once on each request, including revalidation. This is
response validation, not a response cache: domain checks and current DTO
production are not skipped. Body changes change the validator.

Start with `DefaultETagConfig`. `MaxBytes` bounds each captured body;
`MaxConcurrent` bounds active captures for that assembled middleware.
Memory is acquired in bounded pages as bytes arrive. When the byte ceiling is
exceeded, Foundry sends the complete captured prefix followed by later bytes.
When all capture slots are occupied, the response follows the normal handler
path. Neither case silently truncates the body or waits for a free slot.

HEAD does not compute a hash for an unseen GET body and never executes a second
GET handler. Unsafe methods, range-bearing requests, redirects, error responses,
bodyless and partial statuses, existing validators, no-store responses and
streaming protocols retain their handler's behavior. A handler supplying its own
validator remains responsible for its native conditional handling.

Flush transfers pending bytes before flushing the underlying transport.
Full-duplex activation preserves native streaming behavior. Both stop automatic
whole-body hashing. Native writer capabilities stay discoverable without
inventing Flusher, Hijacker or Pusher support, and io.Copy cannot bypass the
body owner. A hijack cannot discard a pending body or unsent final headers.

Ordinary response headers are snapshotted at the first final status. Late
ordinary changes are ignored; declared or explicitly marked trailers remain
distinct and disable automatic hashing. Conditional responses preserve cache,
variation, security and cookie headers according to native HTTP rules.

Observed source errors, cancellation and incomplete declared lengths cannot
become a successfully validated response. A failed transfer after headers have
been sent aborts the connection; already-sent headers cannot be recalled.
Native handlers must still check I/O errors and keep blocking readers responsive
to cancellation. Foundry does not start detached reader goroutines.

[Downloads](http-downloads.md), [streams](http-streams.md) and
[static assets](http-assets.md) retain their own source ownership and file
semantics. Explicit file validators allow native file preconditions before
reading the complete body; automatic hashing requires producing the bytes.

Focused runtime/consumer races, compiler and actual-gopls checks, fuzzing, resource
benchmarks, vet, formatting, generation freshness and documentation checks passed.
The [master acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-08-automatic-etag-focused-acceptance)
records focused verification. The subsequent
[full canonical regression](../../blueprint/00-master-architecture-and-parity.md#milestone-08-automatic-etag-full-acceptance)
also passed. Compression composition subsequently passed its focused and full
regression checks; see the [milestone completion review](../../blueprint/00-master-architecture-and-parity.md#milestone-08-completion-review).
