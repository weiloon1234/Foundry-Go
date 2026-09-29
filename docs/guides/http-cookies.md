# Typed cookies and cookie signing

Declare one cookie with its name, concrete value codec and scope. Reuse the same
declaration for reads, writes and removal:

```go
options := foundryhttp.DefaultCookieOptions()
options.MaxAge = time.Hour
remember := foundryhttp.DefineCookie(
    "remember",
    foundryhttp.BoolCookie[bool](),
    options,
)

choice, err := remember.Read(request)
if err != nil {
    return err
}
if enabled, present := choice.Get(); present {
    // enabled can be false while the cookie is present.
    _ = enabled
}
if err := remember.Set(ctx, writer, true); err != nil {
    return err
}
```

`Cookie[V]` and `SignedCookie[V]` retain V through Read and Set.
`StringCookie`, `IntegerCookie`, `FloatCookie`, `BoolCookie`, `ModelIDCookie` and
`TextCookie` reuse the existing scalar codecs, including named types, model
ownership, generated enum validation, exact decimals and temporal text methods.
`CookieCodec[V]` is that same scalar interface. There is no duplicate model codec.

Read returns `value.Optional[V]`: missing differs from a present empty string
or false value. Names are case-sensitive. The Cookie header is scanned pair by
pair: cookies owned by other applications or scripts (JSON or quoted values,
non-ASCII bytes, nameless pairs, trailing separators) are ignored and never reject
the request. Only the selected name's own value must satisfy the native cookie
grammar; a malformed value of that name is a BadRequest. Repeated occurrences of
the selected name, including identical values, read as **absent**: the server
cannot tell which path or domain set each copy (for example a cookie "tossed" by
a sibling subdomain). Prefer a `__Host-` name for credentials, because browsers
refuse `__Host-` cookies set by other hosts or with a narrower path.

Built-in scalar and credential codecs run under a cheap in-goroutine panic
boundary. Custom, text and enum codecs must be deterministic, bounded and
concurrency-safe; the framework runs them isolated, catches panic/Goexit, waits
for callback exit and preserves request cancellation. The complete request-cookie
input is bounded to 64 KiB, 256 fields and 2,048 pairs; only input beyond those
generous bounds is rejected.

## Scope, lifetime and native HTTP

Defaults are host-only, Path=/, Secure, HttpOnly and SameSite=Lax. Native
`net/http.SameSite` values are reused. For an explicitly supported local HTTP
environment, set Secure=false; request-dependent selection must use the verified
`IsSecure(request)` boundary, not an arbitrary forwarded header.

MaxAge must be a nonnegative whole-second duration. Zero MaxAge and zero Expires
mean a browser session cookie. MaxAge takes precedence when both are supplied.
`Clear(writer)` emits deletion for the exact name/domain/path and retains the
security flags. It does not remove same-name cookies deliberately set at other paths.

`__Secure-` requires Secure. `__Host-` also requires host-only scope and Path=/.
SameSite=None and Partitioned require Secure. Invalid scope/policy declarations
fail Validate and cannot write a response header. Browser/public-suffix policy
can still reject otherwise valid cookies.

Set validates the value and complete field before appending one Set-Cookie header;
other cookies remain present. Call before headers commit. These methods do not
wrap ResponseWriter or buffer a response. Each Set is one operation; a sequence
of calls is not an atomic batch.

Native cookie quoting is preserved. For arbitrary text requiring safe transport
encoding, explicitly use `Base64Cookie(existingCodec)`. This is canonical URL-safe
encoding, not integrity protection or encryption. Complete outgoing fields,
including name and scope attributes, are limited to 4,096 bytes.

## Signed cookies

Create a reusable rotation set using protected configuration values, then inject
the application clock:

```go
keys, err := foundryhttp.NewSigningKeys(
    foundryhttp.SigningKey{ID: "current", Secret: configuredKey},
    // Optional previous keys remain valid during rotation.
)
if err != nil {
    return err
}
signer, err := foundryhttp.NewCookieSigner(keys, applicationClock)
if err != nil {
    return err
}
signedRemember := remember.Signed(signer)
if err := signedRemember.Set(ctx, writer, true); err != nil {
    return err
}
```

SigningKeys supports at most sixteen distinct IDs. IDs contain ASCII letters,
digits, hyphens or underscores. Key material uses `secret.String` and is 32–1,024
bytes; generate it cryptographically. Length alone cannot prove entropy. Key,
key-set, signer and signed-cookie formatting do not reveal key material.

New values use the active key; previous keys verify existing values. Removing a
key invalidates its signatures. HMAC-SHA256 authenticates a versioned envelope
containing the key ID, expiry and encoded value. A separate purpose and scope bind
the signature to the cookie name, domain, path and security flags. Verification
uses constant-time MAC comparison before invoking the domain value decoder.

MaxAge/Expires is also enforced by the signed envelope at whole-second precision;
the exact expiry second is expired. MaxAge still takes precedence. Changing a
declaration's lifetime changes new cookies, not previously signed expiries.
A signed browser-session cookie with no configured expiry has no server-side
expiry. Authentication credentials need their own provider lifetime/revocation.

Malformed, tampered, expired and retired-key values share
`ErrInvalidSignedCookie`, detectable with errors.Is; Read wraps it as BadRequest
for transport handling. A missing signed cookie remains optional. Signed values
are encoded but readable: signing does not encrypt, prevent pre-expiry replay,
authenticate a user or replace an authorization check.

## Encrypted cookies

When a browser must not read the value, encrypt it with the application's
[encryption keyring](../../encryption/doc.go) instead of signing it:

```go
encrypter, err := foundryhttp.NewCookieEncrypter(keyring, applicationClock)
if err != nil {
    return err
}
private := remember.Encrypted(encrypter)
if err := private.Set(ctx, writer, true); err != nil {
    return err
}
```

`EncryptedCookie[V]` keeps V through Read and Set. The encoded value, its
MaxAge/Expires deadline and a version marker are sealed with AES-256-GCM under a
dedicated `foundry.cookie.v1` purpose; the cookie name, domain, path and security
flags are the authenticated binding, so a value cannot be replayed under another
cookie or scope. New values use the keyring's active key; retained keys decrypt
existing values, so rotating the active key keeps cookies readable until the old
key is removed. Each Set consumes one encryption from the active key's bounded
budget, so rotate keys as the [keyring](../../encryption/key.go) describes.

The wire value is the keyring's canonical envelope, which adds about 40 bytes
plus one third for base64url encoding; the complete field must still fit 4,096
bytes. Malformed, tampered, expired, rescoped and retired-key values share
`ErrInvalidEncryptedCookie`, which Read wraps as BadRequest; the value decoder
runs only after decryption succeeds. Like signing, encryption does not revoke
values, prevent replay before expiry or authenticate a user.

The [independent TLS consumer](../../tests/fixtures/consumer/httpcookies/cookies.go)
uses a model-owned selected-user ID, a named locale and a generated enum as
preferences. Its native cookie-jar test verifies typed round trips, server expiry
and exact removal. The example uses explicit raw HTTP handlers for header writes;
it does not imply automatic response-schema inference.

Native syntax follows [Go's Cookie API](https://pkg.go.dev/net/http#Cookie) and
[RFC 6265](https://www.rfc-editor.org/rfc/rfc6265.html); modern scope guidance also
appears in the [HTTP working group's current draft](https://httpwg.org/http-extensions/draft-ietf-httpbis-rfc6265bis.html).
Session/CSRF integration belongs to authentication. Signed URLs and the remaining
transport work are separate additions.
