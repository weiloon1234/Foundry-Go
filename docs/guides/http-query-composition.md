# Typed query composition and defaults

**Status: focused runtime, consumer, compiler and editor acceptance passed.**

Reuse generated filter bindings inside a larger query value. `EmbedQuery`
selects the concrete inner field; `MergeQueries` combines descriptors of the
same outer type. Neither introduces a wire-name prefix nor rewrites codecs.
Overlapping wire names reject declaration rather than choosing a winner.

```go
type SearchWindow struct {
    Filters SearchInput
    Size    int
}

var SearchWindowParameters = foundryhttp.MergeQueries(
    foundryhttp.EmbedQuery(SearchInputDescriptor(),
        func(input *SearchWindow) *SearchInput { return &input.Filters }),
    foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam(
        "size", foundryhttp.IntegerQuery[int](), 20,
        func(input *SearchWindow) *int { return &input.Size },
    )),
)
```

The [independent consumer](../../tests/fixtures/consumer/httpquery/composition.go)
uses its existing generated model-ID, text and enum filters. The composed
`Query[SearchWindow]` can be passed directly to an ordinary typed endpoint.
Its required/repeated fields, scalar metadata, precise value types and escaping
remain the same as the generated inner descriptor.

`DefaultQueryParam` supplies its value only when the key is omitted. Explicit
zero, false and empty text still run through the codec. This helper does not
imply a positivity or size rule: use a suitable codec or the endpoint's typed
validation rules for domain constraints. Duplicate scalar keys and unknown
names reject input before any field/default decoder runs.

A default is snapshotted through its codec during construction and must have a
canonical round trip. Panicking, failing or exiting codec methods produce an
invalid declaration. The retained unescaped spelling must be valid UTF-8 and at
most 16KiB. Every omitted decode parses it afresh, avoiding shared mutable values
between requests. Custom codecs must return owned results and obey the same
bounded, deterministic, concurrent-use rules as other URL codecs.

Defaults are public declaration data. `QueryParameterInfo.DefaultURL` exposes
that exact unescaped URL spelling as
an optional string. A present empty string differs from no default. This is a
URL representation, not a fabricated JSON default or browser implementation of
a custom codec. The codec's normal scalar metadata is preserved; an opaque
custom codec remains opaque. Client exporters can reuse the declared literal
when constructing requests. Encoding writes the actual supplied field value,
including zero or a value equal to the default, rather than silently omitting it.

Composition and decoding return no partially populated query on failure.
Embedding selectors execute inside the existing callback ownership boundary;
nil selectors reject declaration, and selectors returning nil or panicking at
execution produce internal failures. A previously validated default that later
fails to decode is a server failure, since the client supplied no value.
Explicit client-value rejection remains an ordinary safe query error.

[HTTP pagination](http-pagination.md) reuses these primitives and database page
requests, applies defaults and bounds, maps models to declared response DTOs,
and generates filter-preserving links from approved origins.
[Cursor pagination](http-cursor-pagination.md) retains the same ownership rules.

Combined transport full regression passed.
