# Native streaming JSON value contracts

**Status: focused runtime, generation, consumer, compiler and editor acceptance passed.**

Custom values can implement Go's `MarshalJSONTo` and `UnmarshalJSONFrom`
protocols and expose a typed `JSONContract` value method. Generated DTOs discover
that method automatically. Legacy JSON/text methods remain supported.

The [StreamLabel consumer](../../tests/fixtures/consumer/httpdto/stream.go) keeps
its stored representation private. Its contract returns
`contract.JSON[StreamLabel]`, and the response uses StreamLabel directly, in a
slice and inside `Optional[Nullable[StreamLabel]]`. No per-field codec
registration is required.

```go
func (v StreamLabel) MarshalJSONTo(enc *jsontext.Encoder) error {
    return enc.WriteToken(jsontext.String("label:" + v.text))
}

func (v *StreamLabel) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
    var text string
    if err := jsonv2.UnmarshalDecode(dec, &text); err != nil {
        return err
    }
    text, ok := strings.CutPrefix(text, "label:")
    if !ok {
        return fault.New(fault.Invalid, "invalid label")
    }
    *v = NewStreamLabel(text)
    return nil
}
```

Foundry reuses the installed Go decoder and its native method precedence.
Streaming methods take precedence over corresponding legacy value methods.
Native `errors.ErrUnsupported` fallback is supported only under Go's rules:
a method cannot consume input or write output and then request fallback.
A streaming decoder must consume exactly one value.

Delegate nested decoding through `jsonv2.UnmarshalDecode` and encoding through
`jsonv2.MarshalEncode` to preserve the enclosing decoder/encoder options.
Foundry's decoder retains exact `json.Number` values for dynamic numbers and
rejects trailing values. The declared schema still checks input before native
decoding and output before HTTP can publish it.

Custom types require an encoder available on the value and a supported decoder
on its pointer. Pointer-only encoders are rejected because top-level values and
map elements do not have the required addressability under the framework's
native semantics. Persistence models remain excluded from public DTO contracts.

Codec failures return no partial DTO or response bytes. Panics and Goexit are
internal failures; ordinary codec rejection is invalid input. Cancellation
waits for active callback work to finish. Codecs must be bounded, deterministic,
concurrency-safe and must not retain borrowed decoders, encoders or input.

These are JSON VALUE methods. JSON object keys retain their separate native
rules and [typed key contracts](http-json-map-keys.md); streaming value methods
do not become map-key codecs.

Combined transport full regression passed.
