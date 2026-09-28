# Typed model field changes

Generation supplies model-specific change sets for milestone 07. A `User` model gets `UserChanges`, `UserFieldChanges` and `CompareUser`. They build on `database/lifecycle.FieldChange[T]`, retaining concrete before/after values and reporting assignment separately from a change in stored value. [Declared model hooks](model-hooks.md) receive captured changes automatically; constructing a comparison separately does not attach observers to model writes.

## Generated model changes

The generated lifecycle comparison boundary accepts two optional stored models and an effective assignment draft:

```go
changes, err := CompareUser(value.Set(before), value.Set(after), effectiveDraft)
if err != nil {
    return err
}
email := changes.Fields().Email
if email.Changed() {
    previous := email.Before() // value.Optional[string]
    current := email.After()   // value.Optional[string]
    _ = previous
    _ = current
}
```

Here `before` and `after` are complete stored `User` values and `effectiveDraft` is a `UserDraft`. The generated declaration owns every field getter and codec. `Fields()` returns a model-specific field set by value; its field names cannot collide with the change set's `Before()`, `After()`, `Assigned()` and `Changed()` methods. The last two methods report whether any persisted field is assigned or changed.

Creation has an absent before-model; physical deletion has an absent after-model. Soft deletion and restoration retain both stored snapshots. Automatically captured changes expose the lifecycle kind through `Operation()`; standalone comparisons leave that optional value absent. Both present snapshots must have the same primary key, and an existing primary key cannot be assigned. Neither absent/absent snapshots nor an assignment on physical deletion are valid. Comparison errors return an empty change set, with no partial snapshots or field results.

`Before()` and `After()` on the model change set return persisted model snapshots. Generation excludes ignored fields, relation loads and relation aggregates from those copies without modifying input models. The effective draft supplies assignment presence only; its input values are not treated as stored results or retained in change data. Effective assignment state must include framework-generated values, such as an omitted UUID filled by creation, and assignments made by hooks.

The [comparison PostgreSQL consumer](../../tests/fixtures/consumer/mutatorqueries/changes_postgres_test.go) calls this boundary explicitly to verify it against generated writes. The [hook consumer](../../tests/fixtures/consumer/hookqueries/hooks_postgres_test.go) uses automatic snapshot locking and effective-assignment capture; applications should not duplicate that orchestration in handlers. The [consumer snapshot tests](../../tests/fixtures/consumer/model_changes_test.go) cover model identity, private/transient state exclusion and failed comparisons.

## Field-change data

`Before()` and `After()` return `value.Optional[T]`. Absence means the model did not exist on that side of the operation. For a nullable field, `T` is `value.Nullable[V]`, so a present NULL remains distinct from a missing model. `Assigned()` reports whether the effective write assigned that field. `Changed()` compares the stored snapshots, including existence changes.

The framework comparison boundary is:

```go
change, err := lifecycle.CompareField(
    codec.String[string](),
    value.Set("ada@example.test"),
    value.Set("ada@example.test"),
    true,
)
// On success: change.Assigned() is true; change.Changed() is false.
```

`CompareField` and the typed `CompareModelField` getter adapter support generated comparisons. Ordinary model-wide inspection uses the generated field set. The comparison primitives perform no automatic capture or hook registration.

## Comparison and failure behavior

Compare the locked pre-write snapshot with the complete hydrated write result. This includes database defaults and returned trigger changes. The agreed read-accessor contract uses explicit typed getters and preserves stored fields; getter results must not replace either comparison snapshot. Input drafts are not stored snapshots: a mutator may normalize an input into the existing stored value, and an omitted field may receive a database default.

The field's codec validates both present values and supplies their canonical representation. Comparisons preserve exact decimal and canonical JSON values, normalize instant equality and distinguish NULL from zero. Calendar interval months, days and elapsed components remain distinct; the SQL-equivalence key used for relation grouping is not suitable for detecting those changes. The comparison does not execute mutators, accessors or database calls.

At least one model snapshot is required. A physical deletion cannot carry an assignment. Invalid codec values, non-finite custom numeric representations and unsupported driver representations fail without publishing partial change data. `errors.Is` retains codec error identity. A zero `FieldChange` contains no snapshots and does not represent a successful write.

Generated persisted snapshots own their [binary buffers](binary-models.md); other built-in persisted types are immutable values. Custom values with mutable references retain ordinary Go copy semantics and must be treated as read-only. Custom codecs own canonical binding and must use supported finite driver scalars, byte buffers or timestamps. Routine `String`/`GoString` formatting omits snapshot values; deliberate `Before()`/`After()` access returns typed snapshots with fresh copies of built-in binary fields.

See [model hooks](model-hooks.md) for callback order and transaction behavior, and [milestone 07](../../blueprint/07-model-lifecycle-events-and-audit.md) for remaining lifecycle, event and audit requirements.
