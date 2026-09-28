# Managed model timestamps

Declare persisted `CreatedAt` and `UpdatedAt` fields to enable timestamps automatically. Each field can use `time.Time` or `temporal.DateTime`; column tags retain the usual mapping. The [consumer model](../../tests/fixtures/consumer/timequeries/models.go) mixes both types:

```go
//foundry:model table=time_members
type Member struct {
    ID        model.ID[Member]
    Name      string
    CreatedAt time.Time         `foundry:"column=created_on"`
    UpdatedAt temporal.DateTime `foundry:"column=changed_on"`
}
```

The migration owns the non-null timestamp columns. No database default is needed for these framework-managed values. Generation documents the managed behavior beside each handwritten field and in generated descriptors and draft setters, so ordinary gopls hover exposes it automatically.

```go
q := timequeries.QueryTimeMembers()
member, err := q.Create(ctx, db, timequeries.MemberDraft{}.SetName("first"))
if err != nil {
    return err
}
member, err = q.Update(ctx, db, member.ID, timequeries.MemberDraft{}.SetName("updated"))
```

An insert supplies `CreatedAt` when omitted and always supplies `UpdatedAt`. An update always assigns `UpdatedAt`, while leaving `CreatedAt` unchanged unless explicitly assigned. An empty update draft therefore performs a timestamp-only write. The caller's draft remains unchanged.

The two fields must form a valid pair: non-null, non-primary instants with their exact supported Go types. A lone field remains ordinary. `timestamps=true` on the model directive requires a valid pair; `timestamps=false` disables the convention and leaves both fields subject to ordinary draft/default rules. Defined wrapper types and distinct mutator input types are rejected for managed timestamps; Go aliases and same-type field mutators remain supported.

## Clock and precision

Database modules inherit the clock configured by `foundation.WithClock`, including `testkit.Clock`. Direct `database.Open`/`Prepare` and `postgres.Open` default to `clock.System`. Their optional `database.WithClock(source)` argument supplies an explicit override; `database.Module` and `postgres.Module` accept it too.

Foundry samples the actual transaction owner's clock once per executed write attempt. It converts this framework-produced instant to UTC and truncates it to PostgreSQL microsecond precision. Both automatic fields use that instant. A custom clock must support concurrent use. Sessions and nested savepoints preserve their pool's clock; `db.Clock()`, `session.Clock()` and `tx.Clock()` expose the source without freezing time or extending resource ownership.

Explicitly supplied values retain normal codec validation. For example, an explicit `CreatedAt` with sub-microsecond precision fails rather than silently losing data. An explicit `UpdatedAt` is visible to before hooks but is replaced by the managed value. Invalid clock instants fail the write. Connection acquisition, cancellation and cleanup continue to use real context deadlines.

Reads, query/draft construction and empty batch operations do not sample model time. Time is sampled inside the write transaction after before hooks; a rejected attempt may therefore sample the clock without persisting anything. Foundry does not automatically retry writes or promise globally monotonic timestamps across applications.

## Hooks, changes and batches

Normal creation runs `Saving → Creating → timestamps → field mutators → INSERT → Created → Saved`. Updating locks and reads the current model, then runs `Saving → Updating → timestamps → field mutators → UPDATE → Updated → Saved`. Same-type timestamp mutators run once on the effective assignments and their final values must pass the field codecs. Before hooks see the supplied draft; after hooks receive complete stored snapshots, including database-triggered changes.

Automatic assignments participate in [typed changes](model-changes.md): `Assigned()` records that the timestamp was written, while `Changed()` compares stored values. Hook failures, invalid values and failed SQL roll back the owning transaction or savepoint. After-commit behavior follows [model hooks](model-hooks.md). Physical deletion does not update timestamps. [Soft deletion](model-soft-deletes.md) shares one sample for `DeletedAt` and managed `UpdatedAt`; restoration clears `DeletedAt` and updates managed `UpdatedAt`.

`CreateMany`, `Upsert` and `UpsertMany` apply timestamp conventions while retaining their explicit bulk semantics: they skip per-model observers. Each batch shares one clock sample. An upsert update action automatically copies its proposed `UpdatedAt`, preserving its Go normalization and PostgreSQL `BEFORE INSERT` changes without running the Go mutator again. This replaces any explicit update assignment for that field. `CreatedAt` changes on conflict only when the policy explicitly selects it. `DO NOTHING` and update conditions retain their skipped-result behavior.

Conflict targets and original policies must still be valid; an empty update policy does not become valid merely because Foundry could add `UpdatedAt`. Drafts and conflict policies remain reusable. See [typed upserts](model-upserts.md) and the [PostgreSQL timestamp acceptance](../../tests/fixtures/consumer/timequeries/timestamps_postgres_test.go).
