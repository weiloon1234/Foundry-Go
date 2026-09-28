# Native URL floats and callback ownership

Foundry preserves native and named `float32`/`float64` values in generated path
and query bindings. A custom text codec is unnecessary for an ordinary float.
The [independent consumer](../../tests/fixtures/consumer/httpquery/floats.go)
declares these shapes:

```go
type Latitude float32
type Distance float64
type Distances []Distance

//foundry:query
type NearbyInput struct {
    Latitude  Latitude
    Maximum   value.Optional[Distance]
    Distances Distances `query:"distance"`
}

//foundry:path pattern=/positions/{latitude}
type PositionPath struct {
    Latitude Latitude
}
```

Normal generation creates `NearbyInputDescriptor()` and
`PositionPathDescriptor()`. Required, optional and repeated query cardinalities
follow the [query guide](http-queries.md). Field values retain their concrete Go
types during decoding, encoding, completion and hover. Another named type with
the same underlying float type still requires an explicit Go conversion.

For manual descriptors, use `FloatQuery[T]()` or `FloatPath[T]()`. Their shared
codec accepts decimal/scientific notation, including signs, leading zeroes,
`.5`, `1.` and `-2E-3`. It rejects whitespace, digit separators, hexadecimal
notation, malformed numbers, overflow, NaN and infinity. Encoding rejects
nonfinite native values as well.

Parsing uses the destination precision directly, including `float32` rounding
near a midpoint. Formatting emits the shortest decimal that round trips to the
same native value. Signed zero and representable subnormal values are retained;
underflow follows Go's native rounding and may become zero. Native binary floats
do not promise exact decimal arithmetic. Continue using `decimal.Decimal` and
its existing text codec when the decimal representation must remain exact.

Query escaping still occurs exactly once. `1e+20` is encoded as `1e%2B20` in a
query string; a received unescaped `+` becomes a space and fails float parsing.
Path escaping follows the [routing guide](http-routing.md).

A named float's complete `MarshalText`/`UnmarshalText` codec takes precedence
over native float conversion. The same resolver is used for paths and queries,
so a custom representation does not silently change between transports.

## Callback failures and cancellation

Path descriptors own codec and field-selector execution until it finishes.
Panic and `runtime.Goexit` produce internal errors, with no partial URL or model
value returned. Ordinary codec errors remain private causes and are classified
without invoking their `Error`, `Is`, `As` or `Unwrap` methods. A nil field selector
result is an internal error. Direct calls to a custom codec do not add a path
descriptor's recovery boundary.

An already-canceled HTTP request skips path codecs and its handler. Cancellation
during a codec waits for that codec's actual return, skips subsequent fields and
produces the shared timeout response. A simultaneous callback failure retains
its internal classification. Custom codecs must terminate, respect ownership and
be safe for concurrent calls; Foundry does not abandon an active callback.

`WriteError` preserves Go wrapped/joined error classification. It inspects custom
error methods in an owned callback before writing headers or body. A panic or
`Goexit` there produces a safe 500 response and an injected-logger diagnostic
without exposing panic payloads. Error methods must terminate and support
concurrent classification. [Bounded traversal](http-requests.md#typed-public-errors)
prevents cyclic error chains from trapping classification. The native response
writer stays outside the callback.

## Verification scope

Focused runtime and consumer race checks, native float bit-roundtrip fuzzing,
generated path/query runtime checks, deterministic generation, freshness and
three compiler-rejection cases passed for this change. Canonical generation, consumer and actual editor checks also passed. Broader
regression verification also passed. [Endpoint adapters](http-endpoints.md),
[scalar metadata](http-parameter-contracts.md) and [validation](validation.md)
reuse these same typed codecs.
