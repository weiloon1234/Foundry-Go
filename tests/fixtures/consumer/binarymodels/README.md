# Binary models

`Records` separates fresh `Input` from stored `Payload`. Its input columns discover
`MutateBody`, `MutateNote` and `MutateEncoded`; comparisons use stored values.
`Records.RawBytes` binds the stored `Raw` field without shadowing `Table.Raw`.
Assignments and live `orm.WriteGet`/`WriteNullableGet` results own their buffers.
A non-nil empty slice stays present, while nullable `SetNull` writes SQL NULL.

`Views` is an explicit `rows` DTO. Conflict literals use `orm.ConflictSet` and
`orm.ConflictNull`; changes use `orm.Compare` and typed field readers. PostgreSQL
assertions retain byte ownership, empty/null, reusable conflict and hydration checks.
Model and projection generation directives are removed; the structs and typed
ORM/rows declarations own persistence and hydration.
