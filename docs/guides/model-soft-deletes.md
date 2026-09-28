# Soft deletion and restoration

Declare a persisted `DeletedAt` field with `value.Nullable[time.Time]` or `value.Nullable[temporal.DateTime]`. Generation enables soft deletion automatically and documents it beside the actual field, including any custom getter or setter. Column tags retain their usual mapping. The migration owns the nullable timestamp column.

```go
//foundry:model table=members
type Member struct {
    ID        model.ID[Member]
    Name      string
    DeletedAt value.Nullable[time.Time]
}
```

`soft_deletes=true` on the model directive requires a valid persisted `DeletedAt`; `soft_deletes=false` leaves the field ordinary. Managed deletion fields must be non-primary and use one of the exact nullable instant types above. Go aliases and same-type mutators are supported; distinct mutator inputs are rejected because Foundry cannot invent the input for an automatic timestamp.

## Typed writes and visibility

The [independent consumer](../../tests/fixtures/consumer/softqueries/models.go) also declares managed creation/update timestamps, an explicit deletion getter, self-relations and a pivot model. Its generated API supports:

```go
q := softqueries.QuerySoftMembers()
member, err := q.Create(ctx, db, softqueries.MemberDraft{}.SetName("member"))
if err != nil {
    return err
}
deleted, err := q.Delete(ctx, db, member.ID)
if err != nil {
    return err
}
restored, err := q.Restore(ctx, db, deleted.ID)
if err != nil {
    return err
}
_, err = q.WithTrashed().ForceDelete(ctx, db, restored.ID)
```

`Delete` updates the deletion timestamp on one active model and returns its complete stored result. `Restore` selects one deleted model, clears its deletion timestamp and returns the restored result. `ForceDelete` physically removes one model within the query's current visibility and returns its pre-deletion model. All three retain explicit predicates and require the concrete model's primary key; no match returns `database.NotFound`.

Ordinary model queries select active records. `WithTrashed()` includes active and deleted records; `OnlyTrashed()` selects deleted records; `WithoutTrashed()` restores the default. These immutable options preserve explicit predicates, including a caller's own deletion-field predicate. They apply to reads, counts, projections, pagination, iteration, query sources and updates. Locked queries retain both their visibility and transaction lock policy.

`Delete` only changes active records: `WithTrashed().Delete(...)` still selects active records, while `OnlyTrashed().Delete(...)` matches nothing. `Restore` selects deleted records regardless of the implicit visibility previously selected. `ForceDelete` respects the current visibility, so use `WithTrashed()` or `OnlyTrashed()` to physically delete an already-deleted model.

Models without managed soft deletion retain physical `Delete`. Their generated APIs do not add the typed-key `Restore` and `ForceDelete` helpers. Applying trashed visibility or special deletion operations to a declaration without soft-delete metadata fails validation. Creation rejects explicit query predicates and non-default visibility.

## Relations and query composition

Each related model has its own default scope. Changing a parent query's visibility does not expose deleted children or pivots automatically:

```go
r := softqueries.MemberRelations()
members, err := softqueries.QuerySoftMembers().With(
    r.Children.WithTrashed(),
    r.Groups.WithTrashed().OnlyTrashedPivot(),
).All(ctx, db)
```

Direct and through relation descriptors support `WithTrashed`, `OnlyTrashed` and `WithoutTrashed` for the target model. Through descriptors independently support `WithTrashedPivot`, `OnlyTrashedPivot` and `WithoutTrashedPivot` for the pivot. Eager loading, relation predicates and aggregates use the descriptor's same visibility. Construct an aggregate with `query.Related(scopedRelation, ...)` when it should count that scoped relation.

Visibility stays inside each model source during aliasing, CTEs, set operations and joins. A left join to a hidden model retains its unmatched outer row instead of filtering it away. Explicitly typed projections remain DTOs; soft deletion does not turn models into public transport contracts.

## Lifecycle, time and failures

The callback stages are:

- Soft delete: `Deleting → automatic fields → mutators → UPDATE → Deleted`.
- Restore: `Restoring → automatic fields → mutators → UPDATE → Restored`.
- Force delete: `ForceDeleting → Deleting → DELETE → Deleted → ForceDeleted`.

Model-local callbacks run before provider observers within each stage. These operations do not also emit `Saving`, `Updating`, `Updated` or `Saved`. Before callbacks receive the locked stored model; after callbacks receive typed changes. `changes.Operation()` identifies the captured `lifecycle.SoftDelete`, `lifecycle.Restore` or `lifecycle.ForceDelete`. A standalone `Compare<Model>` result has no operation.

Soft deletion and managed `UpdatedAt` share one application-clock sample, normalized to UTC microsecond precision. Restoration clears `DeletedAt` and updates managed `UpdatedAt`; restoring a model without managed timestamps does not need a clock sample. Force deletion does not update timestamps. Same-type mutators run once for assigned values; clearing NULL skips a scalar mutator. See [managed timestamps](model-timestamps.md).

Soft deletion retains both stored snapshots in the change set. Only physical deletion has an absent after-model. Automatic assignments participate in field change tracking. Ordinary draft assignments to `DeletedAt` use normal create/update events; they do not implicitly become `Delete` or `Restore` calls. Exported fields remain stored values, and [custom getters](model-accessors.md) remain explicit.

Hook errors, cancellation and database failures roll back the write or its savepoint. A restore that conflicts with a unique constraint returns the database error and leaves the model deleted. Foreign keys and indexes retain their database semantics: soft deletion does not cascade to relations or remove a row from an ordinary unique index. Use explicit migrations for any partial unique index needed by the domain.

After-commit callbacks follow the [existing transaction contract](model-hooks.md); rollback discards them. Ordinary in-process callbacks do not provide durable delivery. Explicit bulk inserts/upserts still skip per-model observers, and their conflict policy owns conflict updates; default read visibility is not an extra upsert condition. Hook-aware per-row bulk alternatives, event/outbox dispatch and audit remain milestone 07 work.

The [lifecycle consumer](../../tests/fixtures/consumer/softqueries/lifecycle_postgres_test.go) and [visibility consumer](../../tests/fixtures/consumer/softqueries/visibility_postgres_test.go) exercise these public contracts. Current verification status is recorded in the [master blueprint](../../blueprint/00-master-architecture-and-parity.md).
