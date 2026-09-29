# Typed query parameters

**Status: typed query binding and generation are available.** Focused runtime,
generator and independent consumer checks passed, including six additional
compiler-rejection cases. Canonical generation, all freshness targets, consumer
checks and actual gopls completion/hover/definition probes also passed. Full
repository regression verification also passed, including real PostgreSQL, actual
gopls and generated-output freshness. [Typed endpoints](http-endpoints.md) use
these descriptors for their concrete query input.

Declare query fields in an ordinary Go struct and run the shared Foundry
generator. The same generated descriptor decodes request queries and encodes
queries for links. The consumer owns field types and names once.

```go
//foundry:query
type SearchInput struct {
    User     model.ID[models.User]
    Search   value.Optional[string] `query:"q"`
    Statuses []models.Status         `query:"status"`
}

parameters := SearchInputDescriptor()
limits := foundryhttp.QueryLimits{Bytes: 4096, Pairs: 64, Issues: 8}

input, err := parameters.Decode(ctx, request.URL.RawQuery, limits)
if err != nil {
    return err
}

// input.User retains model.ID[models.User].
// input.Search distinguishes omission from an explicitly empty string.
// input.Statuses is an ordinary []models.Status.
encoded, err := parameters.Encode(ctx, input, limits)
```

Here `model`, `value` and `foundryhttp` are framework imports; `models` is the
application's model package. Input and output strings exclude the leading `?`.
Encoding produces query text, not a complete URL. Named-route and endpoint URL
composition must preserve escaping and must not trust incoming hosts.

| Field declaration | Input behavior |
| --- | --- |
| `Name string` | Exactly one `name` is required; an empty value is preserved |
| `Name value.Optional[string]` | Missing is omitted; `name=` is present and empty |
| `Page value.Optional[uint16]` | Missing is omitted; `page=0` is present; overflow fails |
| `Active value.Optional[bool]` | Uses exactly `true` or `false`; bare `active` fails |
| `Statuses []Status` | `statuses=active&statuses=disabled` supplies two typed values |
| `User model.ID[User]` | Preserves model ownership and rejects an empty identity |

Fields default to snake case. `query:"name"` changes the exact, case-sensitive
name; `query:"-"` skips a field. Bound fields must be exported and non-embedded.
The declaration itself must be an exported, non-generic defined struct. Query
tags do not accept comma options. Dots and brackets in explicitly declared names
are literal characters, so `query:"filter[name]"` describes that single name.

Slices retain input order. A missing repeated key produces nil; a present empty
value is one item for its codec to decode. Nil and empty slices both encode as
omission. There is no implicit comma splitting, nested object expansion or query
null literal. `value.Nullable` and `Optional[[]T]` do not acquire an invented URL
representation. A named value with an explicit text codec can define its own
single-value representation.

## Scalar codecs and generated declarations

Integers require canonical decimal text and preserve their width. Generated
enums use their generated text methods, including membership validation on both
input and output. Model IDs, exact decimals, temporal values, named scalars,
aliases and complete custom text codecs retain their concrete Go types.

Path and query generation share scalar-codec discovery. Query values use query
escaping, so a space encodes as `+` and a literal plus as `%2B`. Path segment
restrictions do not apply to query values. Invalid UTF-8 and malformed percent
escapes fail without replacing data. Business validation owns any further
restrictions on empty text or control characters.

A custom value's complete `MarshalText() ([]byte, error)` and
`UnmarshalText([]byte) error` methods take precedence over its underlying scalar
or slice shape. Partial or incorrectly typed text methods fail generation.
Pointer fields and unsupported ordinary values require a concrete supported
value type. Native and named floats use `FloatQuery[T]()` with destination-width
rounding and finite-value checks. See [URL scalar behavior](http-url-scalars.md)
for scientific notation, query escaping and exact decimal alternatives.

Generation emits `search_input_foundry.gen.go` and records ownership through the
existing shared manifest. Handwritten variables and methods may reference
`SearchInputDescriptor()` on a fresh checkout. Output is deterministic and stale
checks do not write files. Invalid declarations or complete-package type errors
fail before publication. No handwritten registration table duplicates the tags.

For explicit runtime construction, `DefineQuery`, `QueryParam`,
`OptionalQueryParam` and `RepeatedQueryParam` bind concrete field selectors to
typed codecs. Generated descriptors use those same constructors. `Parameters()`
returns an owned, sorted snapshot of names, cardinality and codec-owned scalar
metadata; see [parameter contracts](http-parameter-contracts.md). Undescribed
custom codecs remain explicit rather than acquiring an invented schema.

## Failures and resource ownership

Malformed wire input, unknown names, missing required fields and duplicate
scalar keys are rejected before custom codecs execute. Percent-escaped spellings
of the same name still count as duplicate keys. Every decoding failure returns
zero `SearchInput`; every encoding failure returns an empty string.

`QueryError` retains a safe message and owned field issues. Issue paths use JSON
Pointer syntax over declared names and repeated-item indices. Unknown names
produce a root issue; submitted names and values are not echoed. Internal codec
causes are retained for deliberate diagnostics, not public response messages.
The diagnostic count is capped by `Issues`.

`Bytes` bounds raw input and retained encoded output. `Pairs` bounds input slots,
including empty slots between ampersands, and repeated output values. Length and
pair checks precede decoding allocations. Encoding checks native text lengths
before UTF-8 scanning and escaping; one temporary escaped component can require
up to three times its native byte length.

Codecs and field selectors must be deterministic, safe for concurrent calls on
independent inputs, and return owned values. Their internal work and allocations
remain their responsibility. Cancellation is checked around bounded work and
after a custom method returns. Codecs run on the caller's goroutine: panics
become internal failures, and `runtime.Goexit` ends that goroutine as any Go
call does. Inputs must remain unchanged
until encoding returns.

[Typed endpoints](http-endpoints.md), [validation](validation.md),
[HTTP pagination](http-pagination.md) and endpoint metadata build on this boundary.
Calling `Decode` alone does not perform model lookup or authorization. Client
exporters consume the transport declarations in milestone 21.
