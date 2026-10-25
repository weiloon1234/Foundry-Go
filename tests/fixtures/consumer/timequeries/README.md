# Timestamp query fixture

`Members` declares managed `CreatedAt` and `UpdatedAt` roles and a mutated `Name`
input. `Manuals` opts out with `orm.NoTimestamps()` and ordinary time columns.

ORM tests retain the owning clock, one sample per write/batch, hook ordering,
explicit creation times, normalized upsert times, trigger RETURNING values, vetoes,
savepoints and clock panic/Goexit isolation. `State.ORMCreatedAt` and `ORMUpdatedAt`
copy callback input while the write is live; `ORMChanges` records stored snapshots.
Legacy draft state, hooks and model-generation directives are removed.
