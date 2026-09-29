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

Start with `DefaultETagConfig` (10 MiB `MaxBytes`, 64 `MaxConcurrent`, 100 ms
`AdmissionWait`). `MaxBytes` bounds each captured body. A body written once with
its declared `Content-Length` — typed JSON responses — is hashed in place and
served without a capture copy or a capture slot. Captures up to one 32 KiB page
need no slot either. Larger captures need one of `MaxConcurrent` slots per
assembled middleware; a request waits up to `AdmissionWait` (0–5 s, also ended
by the request context) and then follows the normal handler path without a
validator. Worst-case large-capture memory is `MaxConcurrent × MaxBytes`.
Memory is acquired in bounded pages as bytes arrive. When the byte ceiling is
exceeded, Foundry sends the complete captured prefix followed by later bytes.
Neither case silently truncates the body.

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

## Cache-Control policy

`CacheControl(policy)` declares a typed `Cache-Control` for GET and HEAD
responses and composes with ETags, so clients revalidate cheaply with 304:

```go
policy := foundryhttp.CachePolicy{
    Visibility:           foundryhttp.CachePublic,
    MaxAge:               time.Minute,
    SharedMaxAge:         value.Set(5 * time.Minute), // s-maxage
    StaleWhileRevalidate: 30 * time.Second,
}
handler, err := foundryhttp.ApplyMiddleware(router,
    foundryhttp.CacheControl(policy),
    foundryhttp.ETags(foundryhttp.DefaultETagConfig()),
)
```

`CachePrivate` limits storage to the requesting browser; `CachePublic` also
admits shared caches. Durations are whole seconds from zero through one year.
`SharedMaxAge` requires public visibility. `NoCache` stores but revalidates each
use, `MustRevalidate` forbids serving stale content after expiry, and `Immutable`
requires a positive `MaxAge` without `NoCache`. `NoStoreCachePolicy()` forbids
storage and cannot be combined with any other directive. `ExpiresFrom` adds an
`Expires` date of now plus `MaxAge` from that application clock for HTTP/1.0
caches. Invalid policies fail assembly.

The policy is set before the next handler runs. It never replaces a
`Cache-Control` value an outer middleware already chose (such as a browser
session's `no-store`); a handler may still override it, and shared error
responses replace it with `no-store`. A public policy is downgraded to private
for requests that carry an `Authorization` header, so shared caches never store
credentialed responses. Other methods are unchanged, and 304 responses keep the
declared policy. Install it per route or scope with `WithMiddleware` when
different endpoints need different lifetimes.
