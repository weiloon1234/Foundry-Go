# Typed model field changes

`orm.Changes[M]` contains persisted snapshots and assignment metadata. Hooks receive it automatically after complete `RETURNING` hydration. `orm.ChangeOf` and `orm.NullableChangeOf` return `lifecycle.FieldChange[V]` and `lifecycle.FieldChange[value.Nullable[V]]` respectively.

<!-- orm-example: changes-after -->
```go
func changes(changes orm.Changes[hooks.Member]) (bool, error) {
	field, err := orm.ChangeOf(changes, hooks.Members.Name)
	return field.Changed(), err
}
```

## Stored snapshots and assignments

`Assigned()` reports effective assignment presence, including hook changes, generated identity and managed timestamps. `Changed()` compares stored values. A mutator can normalize a supplied input to the existing stored value: that field is assigned but unchanged. Omitted fields can change through database defaults or triggers.

`Before()` and `After()` return optional stored snapshots. Creation has no before-model; physical deletion has no after-model. Updates, soft deletes and restores retain both. `Operation()` identifies automatic lifecycle capture; standalone comparisons leave it absent.

For a comparison outside a write:

<!-- orm-example: stored-comparison -->
```go
func compareStored(before, after hooks.Member) (orm.Changes[hooks.Member], error) {
	return orm.Compare(hooks.Members.Table, value.Set(before), value.Set(after),
		hooks.Members.Name.Set(after.Name))
}
```

`orm.Compare` requires at least one stored model. Present snapshots must have the same primary identity. Physical deletion cannot carry assignments. This operation does not execute SQL, mutators or accessors and does not dispatch observers.

## Equality and failures

Field codecs validate present values and provide canonical comparison values. Exact decimals, JSON, instants, NULL and zero retain their distinctions. Calendar interval months, days and elapsed components remain separate. Assigned input values never substitute for stored snapshots.

Comparison errors return no partial changes. Safe diagnostics retain cause identity through `errors.Is` and `errors.As` without formatting model values or codec error text. A zero change value is not proof of a successful write.

Built-in binary snapshots own their buffers; custom mutable values retain normal Go copy semantics and must be treated as read-only. Routine formatting hides snapshots. Explicit `Before()` and `After()` access returns their typed values. Audit uses these same stored changes, without a second comparison or getter evaluation. See [audit](audit.md) and [hooks](model-hooks.md).

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
