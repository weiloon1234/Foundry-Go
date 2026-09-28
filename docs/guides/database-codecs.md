# Typed database values

`database/codec` preserves concrete Go types at binding and scanning boundaries. It supplies codecs for named scalars, model-owned IDs, exact decimals, temporal values and nullable fields. Generated enums expose `StatusCodec()`-style functions and delegate their standard SQL interfaces to that same codec. Generated [model reads](model-queries.md) and [writes](model-writes.md) reuse these codecs for predicates, mutation assignments and full-row hydration.

`codec.Interval()` preserves separate calendar and elapsed components through all four PostgreSQL output styles. It rejects infinity and elapsed components outside Go's duration range. Generated interval fields also provide typed calculations and summaries; see [interval queries](interval-queries.md).

## Binding, scanning and validation

`codec.Codec[T].Bind(value T)` validates a value and produces a SQL driver value. `Scan(destination *T)` creates a standard SQL scanner that assigns only after successful decoding. `Decode(source any)` is an explicit driver integration boundary and returns a zero T on failure. A codec's zero value is invalid. There is no reflection-based model-field lookup.

The [consumer codec tests](../../tests/fixtures/consumer/codecs_test.go) and [PostgreSQL consumer test](../../tests/fixtures/consumer/postgres_test.go) exercise these public APIs. For a generated enum:

```go
statusCodec := models.StatusCodec()
bound, err := statusCodec.Bind(models.StatusActive)
// Pass bound as a SQL parameter after checking err.
var status models.Status
scanner := statusCodec.Scan(&status)
// Pass scanner as a scan destination for the matching column.
```

The driver value and SQL column order remain runtime boundaries. A whole row scan can fill earlier destinations before a later column fails, even though each individual codec assigns atomically. Discard the entire attempted row on failure. Generated model queries construct a fresh model and publish it only after every persisted column decodes successfully.

`Validated(func(T) error)` composes one rule at both boundaries. Generated enum codecs reuse their generated `Validate` method, keeping membership declarations in one place. `codec.New` supports explicit custom encoding/decoding; callbacks must be concurrent-safe, preserve inputs and return owned decoded data. Custom callbacks own their validation and NULL semantics. Built-in failures omit the input value; do not assume custom errors or unwrapped database causes are safe to log.

When a custom encoder returns `[]byte(nil)`, `Bind` canonicalizes it to SQL NULL, matching PostgreSQL. A non-nil empty `[]byte{}` remains non-null empty bytes. Binding copies non-null byte buffers so later changes to the encoder's buffer cannot change the bound value. Stored snapshots, change tracking and audit preserve this NULL/empty distinction; a model identity cannot be SQL NULL.

## Scalar and nullable semantics

| Constructor | Contract |
| --- | --- |
| `String[T ~string]()` | Retains named strings and natural keys; accepts driver strings/bytes, validates UTF-8 and rejects NUL |
| `Bool[T ~bool]()` | Accepts native driver booleans, without text or numeric truthiness |
| `Signed[T]()` | Binds signed SQL integers and checks the concrete Go width when decoding |
| `Unsigned[T]()` | Checks both Go width and PostgreSQL's signed bigint limit; negative values fail |
| `Float[T]()` | Preserves an approximate float type; rejects non-finite values and narrowing overflow/underflow |
| `ID[M]()` | Retains model ownership and canonical UUID text; nil UUID is a value, not SQL NULL |
| `Nullable(base)` | Adds SQL NULL while retaining the base codec's validation for present values |
| `JSON[T]()` | Immutable typed JSON payload stored as PostgreSQL JSONB, with strict bounded shape validation |

Integers accept driver `int64` or decimal integer text/bytes. Floats accept driver `float64`; they do not silently convert exact integers or numeric text to an approximation. Ordinary float32 rounding is allowed. Incoming bytes are never retained by the built-in decoders.

`Nullable` returns `Codec[value.Nullable[T]]`: SQL NULL becomes `value.Null[T]()`, and present zero remains `value.Of(zero)`. An omitted mutation field is separate and stays in `value.Optional`, as used by generated drafts. `codec.ID[User]().Scan(&orderID)` and binding an integer through a named string codec fail compilation. Runtime validation still checks incoming bytes, enum casts and physical database constraints.

## Exact decimals

`decimal.Decimal` is an immutable, comparable finite value. Its zero value is numeric zero; `==` and `Cmp` agree with numeric equality. `Parse` accepts plain decimal text with an optional sign, normalizes redundant zeros and rejects exponent notation, whitespace, infinity and NaN. `decimal.MaxDigits` bounds input digits and arithmetic results. `FromInt64` is exact; there is no floating-point constructor.

`Add`, `Sub` and `Mul` return exact values or a bound error without modifying operands. `Cmp` compares numerically and `Scale` reports canonical fractional digits. There is no implicit rounding or division policy. Scale used for display is not retained: `1.00` and `1` are the same value. JSON uses strings to preserve exact digits for JavaScript and other consumers; numeric JSON input is rejected.

`codec.Decimal()` binds canonical text and accepts exact text/integer driver values. Floating-point sources fail. The generator recognizes `decimal.Decimal`, including nullable forms, and emits ordered predicates and typed draft setters. The [ledger declaration](../../tests/fixtures/consumer/models/ledger.go) demonstrates this shape; a float cannot be passed to its amount setter.

The codec cannot override a database column's declared scale. PostgreSQL rounds inputs to a constrained numeric scale; use a matching schema and validate an application's precision/scale rule before writing when rounding is unacceptable. [PostgreSQL numeric types](https://www.postgresql.org/docs/18/datatype-numeric.html)

## Temporal semantics

`Time()` maps Go `time.Time` and `DateTime()` maps Foundry's UTC instant to `timestamptz`. Both normalize UTC and discard monotonic readings. `Date()` preserves a calendar date, `WallTime()` preserves a time of day, and `LocalDateTime()` preserves wall components without inferring an instant or timezone.

These codecs accept years 1–9999 and microsecond precision. Sub-microsecond timestamps fail rather than being silently truncated. Absent dates/local date-times fail; zero wall time is midnight and zero instant is Go's year-one instant. Infinity, leap seconds and PostgreSQL's 24:00 wall-time representation are outside the Foundry value contract. PostgreSQL date/time storage has its own ranges and precision limits. [PostgreSQL date/time types](https://www.postgresql.org/docs/18/datatype-datetime.html)

## Verification

Unit tests check width/representation failures, nullable states, enum validation, failed-scan destination preservation, buffer ownership and temporal meaning. Decimal arithmetic is compared with `math/big.Rat`, including a targeted fuzz run. Independent consumer tests require compiler failures for wrong ID owners, wrong codec values, float decimal mutations and text operators on decimals.

`make test-postgres` verifies exact round trips for every built-in family, including high-precision numeric, UUIDs, all temporal values, named scalar values, SQL NULL and present zero. It also rejects stored overflow, invalid generated enums and unsupported numeric representations without replacing destinations. The independent consumer additionally exercises these codecs through complete generated model reads.

## Binary ownership

[Binary model fields](binary-models.md) use `Bytes[T ~[]byte]()` and explicit nullable wrappers. Binding, hydration, drafts, predicates and change snapshots copy their byte buffers. Non-nil empty slices remain present; the built-in codec rejects nil. `Clone` and `CloneOptional` apply ownership without binding or validation.
