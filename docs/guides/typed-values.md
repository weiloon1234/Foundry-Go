# Typed foundational values

These values are ordinary Go types visible to the compiler and gopls. The [external consumer test](../../tests/fixtures/consumer/values_test.go) demonstrates model IDs and a nullable date patch using public imports.

## Model identity

Declare `ID model.ID[User]` on a `User` model. `model.NewID[User]()` generates an identity; `NewIDAt[User](clock.Now())` takes explicit time for deterministic timestamp behavior. IDs remain comparable and usable as map keys even when the model contains slices or maps. An order ID cannot be passed as a user ID or converted directly between those instantiated ID types.

New identities use UUIDv7: millisecond timestamp plus cryptographic random bits. Ordering within a millisecond, after clock rollback, and between hosts is not promised. The representation follows [RFC 9562 section 5.7](https://www.rfc-editor.org/rfc/rfc9562.html#section-5.7); randomness comes from [Go crypto/rand](https://pkg.go.dev/crypto/rand#Read).

`ParseID[M]` accepts canonical 8-4-4-4-12 hexadecimal UUID text, including existing UUID versions and the nil UUID. `Bytes`/`IDFromBytes` are explicit binary boundaries. Decoding reapplies a model type; it does not establish record existence or authorization. JSON is a string and rejects null. The zero ID is the nil UUID; persistence/request validation decides where it is allowed.

## Omission, zero, and null

| Go value | Meaning |
| --- | --- |
| `value.Optional[T]{}` | Field omitted |
| `value.Set(T)` | Field present, including T's zero value |
| `value.Null[T]()` or `value.Nullable[T]{}` | Explicit null |
| `value.Of(T)` | Non-null value, including T's zero value |
| `value.Optional[value.Nullable[T]]` | Omitted, explicit null, or a concrete T |

`Optional.Get()` returns `(T, isSet)`. `Nullable.Get()` returns `(T, isNonNull)`. Both preserve the concrete value type. Generated create/update drafts will use these distinctions instead of treating zero as an omitted field.

Use `json:",omitzero"` on optional DTO fields. An absent `Optional` cannot encode as a standalone JSON value. An explicit null requires `Nullable`; decoding null into `Optional[int]` fails. Failed decoding leaves the receiver unchanged. A present nil pointer/map that would accidentally encode as null is rejected during serialization; use the explicit nullable type.

JSON decoding into an existing DTO follows normal Go behavior: absent object keys do not reset existing fields. Decode each independent request into a fresh DTO. [Database codecs](database-codecs.md) and [generated drafts](model-generation.md) preserve these states at persistence boundaries; the value types alone do not execute SQL.

## Temporal boundaries

| Type | Meaning and zero behavior |
| --- | --- |
| `temporal.DateTime` | UTC instant; zero is Go's year-1 zero time |
| `temporal.Date` | Calendar date without a zone; zero is absent and cannot serialize |
| `temporal.Time` | Wall time without date/offset; zero is midnight |
| `temporal.LocalDateTime` | Date and wall time without a zone; zero is absent |

Parsing is strict; supported calendar years are 1–9999 and fractions have at most nanosecond precision. Instants require RFC3339 offsets and normalize to UTC without a monotonic clock reading. Dates reject normalization of invalid days. Wall/local values reject offsets, leap seconds, and 24:00. JSON values are strings and reject null; wrap nullable fields explicitly.

Value arithmetic returns a new value and checks the supported date range. `Date.AddDays` changes calendar days. `DateTime.Add` applies elapsed duration to an instant. `LocalDateTime.Add` applies wall-time arithmetic without implying a timezone.

The [application timezone service](application-timezone.md) binds these same values
to an injected clock and configured zone. It changes local calendar/presentation
defaults; `DateTime` persistence and JSON remain UTC.

`instant.LocalIn(zone)` converts an instant into wall fields. `local.In(zone)` requires a unique matching instant: nonexistent DST wall times fail with `fault.Invalid`, repeated times with `fault.Conflict`. Use an explicitly offset `ParseDateTime` input when disambiguation is intended. The implementation checks timezone transitions instead of inheriting [Go time.Date's unspecified choice at DST transitions](https://pkg.go.dev/time#Date). Supported timezone offsets lie within ±24 hours. Machine-local timezone is never inferred.

The application clock is supplied separately by `clock.Clock` and `testkit.Clock`; temporal values do not mutate global time or location settings.

Dynamic JSON numbers decoded inside `Optional` and `Nullable` use `json.Number`,
including nested wrappers. This matches typed JSON snapshots and transport DTOs
and avoids rounding large integers through `float64`.
