# Typed temporary URLs

**Status: signed route and endpoint APIs passed focused acceptance.**
Their combined transport full regression passed.

Create a URL signer from the same immutable `SigningKeys` rotation set used by
[cookies](http-cookies.md), with an injected application clock. No key is inferred
from an application name or environment global. Ordinary formatting redacts key
material. Configure scope, middleware, validation and limits on a route/endpoint
before binding its signer.

The [independent consumer](../../tests/fixtures/consumer/httpsigned/links.go)
uses existing generated model-owned paths, query fields and response DTOs:

```go
signer, err := foundryhttp.NewURLSigner(keys, applicationClock)
if err != nil {
    return err
}
preview := Preview.Signed(signer)
link, err := preview.URL(ctx, publicOrigin,
    httpkernel.UserPath{User: userID},
    httpquery.SearchInput{
        User: userID,
        Search: value.Set("中文 + a/b"),
        Statuses: []models.Status{models.StatusActive},
    },
    expiresAt,
)
```

This excerpt uses the consumer's imports and declarations. `preview.Handle`
requires the same concrete path, query, body and response types as `Preview.Handle`.
Raw routes use `route.Signed(signer).URL(ctx, origin, path, expiresAt)` and
`HandleRaw`. Neither API accepts an arbitrary untyped query map.

The same signed descriptor generates links and registers verification. Reusing
the original unsigned descriptor for registration deliberately creates an
unsigned route; the framework does not guess authorization from a matching path.

## Admission, signing and decoding

Run [PublicURLs](http-public-urls.md) outside the router. When behind a trusted
proxy, run `TrustedProxy` before `PublicURLs`. Missing public-origin admission
is a server configuration error (500). A malformed or unapproved incoming origin
is rejected by that middleware before the signed route executes.

Signatures bind the route ID, declared method, pattern, actual approved public
origin, exact escaped path, original query bytes and expiry. Hostname casing and
default ports use the shared origin normalization. A canonical alias used for
ordinary URL generation does **not** make signatures portable between admitted
origins: the request must arrive at the origin in the signed link. Proxy headers
only supply that origin when the peer is explicitly trusted.

Verification happens before path/query/body codecs, validation and domain handlers.
Typed endpoints receive only their original application query. Native
`URL`, `RequestURI`, headers, body and response writer remain unchanged; raw
handlers can still inspect the original signing parameters. Middleware outside
the verifier retains its normal position and executes before verification.

GET signatures also permit HEAD through Go's GET routing semantics. A separately
declared HEAD route has its own signing scope. Other methods remain exact. TRACE
cannot declare a signed URL.

## Wire and failure contract

Foundry appends two reserved parameters in this order:

```text
?expires=1789387260&signature=v1.rotation-key.base64url-hmac
```

An existing application query precedes these parameters with `&`. The signature
is always last. Declaring `expires` or `signature` as a signed endpoint's domain
query field fails registration, including optional fields that happen to be absent.

Expiry is required and measured in whole Unix seconds. Generation rejects an
expiry at or before the current second, or outside the supported 1970–9999 range.
Verification rejects at `now >= expires`. Removing a previous key invalidates
its links; a rotation set signs new links with its active key and verifies retained
previous keys. Cookie and URL MAC purposes are distinct.

Duplicate or encoded aliases of reserved parameters, noncanonical reserved values,
malformed query escapes, extra data after the signature and changed path/query
spelling fail verification. Reordering repeated values or changing `+` to `%20`
invalidates the link even if another URL parser considers them equivalent.
Generation preserves the original descriptor's escaping; it never normalizes a
signed query after authentication. URI equivalence has several levels; this
framework deliberately authenticates the exact path/query representation.
[RFC 3986, comparison](https://www.rfc-editor.org/rfc/rfc3986#section-6)

The complete absolute URL is bounded by `MaxSignedURLBytes` (64 KiB); at most
`MaxSignedURLQueryPairs` (1024) query slots include the signing envelope.
Typed endpoint query limits also apply to the decoded application query.
Malformed, expired, tampered and retired-key links share a 403 error response;
internal clock/configuration failures remain server errors. The internal
`ErrInvalidSignedURL` sentinel can be recognized with `errors.Is`.
MAC comparison uses the shared standard-library `hmac.Equal` implementation.
[Go HMAC documentation](https://pkg.go.dev/crypto/hmac#Equal)

A signed URL grants integrity of the URL, not confidentiality, authentication,
request-body integrity or one-time use. Repeated use remains valid until expiry.
Keep secrets out of query parameters and use separate domain state when consuming
an action exactly once. A URL may protect a download or preview while the handler
still applies its normal authorization and validation rules.

## Verified coverage

Focused checks include HTTP races, native HTTP consumer requests, bounded
verification fuzzing, shared cookie regressions, exact expiry, key rotation,
purpose separation, query/path tampering, duplicate aliases, proxy origins,
native transport ownership and metadata snapshots. Four compiler-rejection
cases preserve model IDs, query types, handler types and origins; two real-gopls
probes inspect the concrete signed APIs. No provider integration is required.


## Authenticated signatures (milestone 10)

Required and optional typed authentication endpoints and native route adapters
now expose `Signed(signer)`. Their signatures reuse this guide's origin, purpose,
expiry and key rules; authentication still requires its own live credentials.
An authenticated signed endpoint can also use `modelbinding.BindAuthenticated`
without losing its subject type, resource type or DTO contracts. See
[authentication](authentication.md#signed-endpoints-and-bound-resources).
The integration tests passed milestone 10 acceptance.
