# Binary models

`Records` separates fresh `Input` from stored `Payload`. Its input columns discover
`MutateBody`, `MutateNote` and `MutateEncoded`; comparisons use stored values.
`Records.Raw` binds the stored `Raw` field.
Assignments and live `orm.WriteGet`/`WriteNullableGet` results own their buffers.
A non-nil empty slice stays present, while nullable `SetNull` writes SQL NULL.

`Views` is an explicit `rows` DTO. Conflict literals use `orm.ConflictSet` and
`orm.ConflictNull`; changes use `orm.Compare` and typed field readers. PostgreSQL
assertions retain byte ownership, empty/null, reusable conflict and hydration checks.
The model’s `//foundry:model` directive generates its typed table. The handwritten
`rows` declaration owns projection hydration. The configurable table builder in
`declaration_test.go` retains the buffer-ownership tests’ local hooks.
