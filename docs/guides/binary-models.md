# Binary model values

Binary models passed native PostgreSQL, ownership, consumer, compiler,
generator and editor acceptance. See [production acceptance](../production-acceptance.md).

Persisted `[]byte`, named byte-slice types such as `type Payload []byte`, aliases
of those types, and `value.Nullable` wrappers use PostgreSQL `bytea`. A slice
whose element is a distinct named octet type is not `~[]byte` and cannot use
`codec.Bytes` or `orm.BytesOf`. Binary fields cannot be primary or relation keys
because Go requires those key values to be comparable.

`codec.Bytes[T]()` preserves the exact slice type. A non-nil empty slice is a
present empty value; nil is invalid. Use `codec.Nullable(codec.Bytes[T]())` for
SQL NULL. Decoding accepts driver `[]byte`, copies its buffer, and rejects text
coercion. Binding owns its returned buffer. The existing custom-codec boundary
still normalizes a custom encoder's nil byte slice to SQL NULL.

Binary fields expose `orm.ColumnEq`, `orm.ColumnNe`, `orm.ColumnIn`, `orm.ColumnAsc`,
`orm.ColumnDesc`, `orm.ColumnExpr`, `orm.ColumnSet`, counts, typed subquery membership
and conflict operations. Nullable fields add `orm.NullableColumnIsNull`,
`orm.NullableColumnIsNotNull` and `orm.NullableColumnSetNull`; their selected value retains
`value.Nullable[T]`. Binary equality uses stored bytes, not slice identity.
Text matching, numeric arithmetic and relation-key APIs are not exposed.

Assignments copy binary inputs, and getters return independent copies.
`orm.ColumnSet(Records.RawBytes, []byte{})` selects an empty value,
`orm.NullableInputColumnSetNull(Records.Note)` selects NULL for a nullable field,
and omitting the assignment omits a write. Same-type and distinct-input
mutators retain the existing once-per-write lifecycle. A byte-slice input to a
string or other stored field receives the same copying behavior.

Predicates, ordinary assignments and typed binary mutation inputs own
their captured buffers. Reusing a draft or mutation starts with its captured
input. Change tracking copies persisted binary fields and returns fresh model
and field snapshots from `Before` and `After`. Mutable input values remain
ordinary Go slices outside these capture boundaries; synchronize concurrent
application access in the usual way.

The shared `Codec.Clone` and `codec.CloneOptional(c, v)` operations apply ownership without
encoding, decoding, validation or mutation. `Validated`, nullable and metadata
adapters preserve that policy. Custom codecs and custom struct inputs retain
their existing ordinary Go value semantics; referenced application data must
remain read-only. This is not arbitrary deep-copy or reflection-based model
serialization.

See the [consumer model and projection](../../tests/fixtures/consumer/binarymodels/record.go),
[snapshot cases](../../tests/fixtures/consumer/binarymodels/record_test.go), and
[PostgreSQL persistence cases](../../tests/fixtures/consumer/binarymodels/postgres_test.go).
