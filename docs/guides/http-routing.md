# Typed routes and named URLs

Route descriptors bind a semantic ID, method and concrete path type. Reuse one
descriptor for registration and URL generation. The independent
[consumer fixture](../../tests/fixtures/consumer/httpkernel/routes_test.go)
demonstrates this API through real HTTP requests.

For ordinary path structs, [generate the bindings](http-path-generation.md) from
`//foundry:path` declarations. The explicit primitives below remain useful for
custom `PathCodec` implementations.

```go
type MemberPath struct {
    Member model.ID[Member]
}

var MemberShow = foundryhttp.DefineRoute(
    foundryhttp.RouteSpec{
        ID: "members.show",
        Method: foundryhttp.GET,
        Access: foundryhttp.Public,
    },
    foundryhttp.DefinePath("/members/{member}",
        foundryhttp.Param("member", foundryhttp.ModelIDPath[Member](),
            func(path *MemberPath) *model.ID[Member] { return &path.Member }),
    ),
).Within(foundryhttp.DefineScope("/api", "api"))

location, err := MemberShow.URL(MemberPath{Member: member.ID})
// /api/members/<member UUID>
```

Here `Member` is the consumer's model, `model` is Foundry's model package and
`foundryhttp` aliases Foundry's HTTP package. An ID owned by a different model
cannot be assigned to `MemberPath.Member`. Another concrete path type cannot be
passed to `MemberShow.URL` or its handler. Zero model IDs are rejected at runtime;
decoding an ID does not fetch its model or authorize access.

## Explicit registration

`MemberShow.HandleRaw(handler)` requires a
`func(http.ResponseWriter, *http.Request, MemberPath)` callback. The native
transport objects remain available while the path is already decoded and typed.
Pass its registration to `foundryhttp.NewRouter`. The returned router is a
standard `http.Handler`, suitable for the [HTTP kernel](http-kernel.md) or standard
HTTP testing tools.

`HandleRaw` explicitly leaves request bodies and response payloads under handler
control. Route inspection records that boundary as `Raw: true`; client exporters
cannot infer a typed DTO from arbitrary response writes. [Typed endpoints](http-endpoints.md) reuse these route/path declarations with
generated request/response descriptors, automatic validation and a concrete
domain handler. The common generator now emits
the field selectors above from handwritten path structs, using the same runtime
primitives and grammar.

`NewRouter` validates all registrations before returning a usable router. Duplicate
route IDs, missing parameter bindings, invalid declarations and ambiguous native
patterns return ordinary classifiable errors. Registration order cannot choose a
winner for an ambiguous overlap. There is no global router or default mux mutation.

Every route explicitly declares `Public` or `Guarded`. A guarded declaration may
construct URLs, but handler registration requires the matching typed
[authentication adapter](authentication.md). Unbound guarded routes and unknown
access values are rejected. Ordinary typed endpoints support required/optional
authentication; guarded raw/signed composition remains part of milestone 10.

## Path values and scopes

`StringPath[T]()` retains ordinary or named string types such as `MemberCode`.
`IntegerPath[T]()` checks the concrete signed/unsigned integer width, preserving
named natural-key types. Integer input uses canonical decimal spelling without
whitespace, a plus sign or leading zeroes. `ModelIDPath[M]()` retains UUID ownership.
`BoolPath[T]()` accepts exactly `true` or `false`. `TextPath[T, *T]()` reuses
existing text codecs, including enum membership, exact decimals and temporal
representations. Custom `PathCodec[V]` implementations provide typed `Parse` and `Format` methods.
`FloatPath[T]()` preserves native/named floats with finite-value checks and
destination-width rounding. Codecs and field selectors must be deterministic
and safe for concurrent use. See [URL scalar and callback behavior](http-url-scalars.md)
for float syntax, error recovery and cancellation ownership.

`StaticPath("/health")` declares an exact path with the concrete `NoPath` type.
Parameters use `{name}`. A final `{rest...}` explicitly matches a subtree and can
receive empty text. An ordinary trailing slash matches exactly, including `/`;
it does not implicitly register a subtree.

`DefineScope("/api", "api")` supplies literal path and route-ID prefixes.
`Within` returns a new descriptor, leaving the original intact. Scopes can nest;
reuse the returned route so URLs include the same prefixes as registration.
Path prefixes with parameters belong in a concrete typed path declaration.

URL generation produces a relative path and escapes parameter values once.
Slashes inside a single parameter are encoded; catch-all values retain their
segment boundaries. Empty single parameters, control characters, invalid UTF-8
and dot segments are rejected. A parameter equal to a single slash (`/`) is
also rejected because the native router reserves that value for structural
matching; incoming encoded slash-only segments return 400 before route selection. Catch-all values also reject empty interior or
trailing segments. No incoming Host or forwarding header chooses the URL origin.

## Native behavior and inspection

The native Go router owns method matching, pattern precedence and canonical
redirects. GET also matches HEAD; an explicit HEAD route can override that match.
CONNECT tunneling is outside this origin-form route API. The legacy
`GODEBUG=httpmuxgo121=1` router mode is rejected because it disables these method
and wildcard contracts.

Missing routes and disallowed methods use Foundry's shared JSON error envelope.
A 405 retains the native `Allow` header. Malformed path input returns 400, while
a broken field selector returns a private 500. When run through the kernel, its
safe diagnostic retains the injected logger, route ID and request ID. Panic
payloads are scrubbed before logging. The kernel adds the same request
identity described in [HTTP request handling](http-requests.md). HEAD error
responses omit their bodies. Native relative redirects retain their method and
query semantics.

`Router.Routes()` returns metadata sorted by route ID. `MatchedRoute(ctx)` returns
the selected route's metadata inside its handler. Both return independent
snapshots, including parameter-name slices. Route metadata comes from the same
declaration used by runtime matching and URL generation. `Router.Endpoints()` exposes the registered typed payload, validation and error
metadata; see [endpoint inspection](http-endpoints.md). OpenAPI and TypeScript
exporters arrive in milestone 21.
