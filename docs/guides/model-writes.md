# Typed model writes

`orm.Table[M, K]` creates, updates and deletes complete models with concrete key and assignment types. Columns construct `orm.Assignment[M]` values. Writes use the shared mutation compiler, codecs and complete `RETURNING` hydration.

## Create and update

<!-- orm-example: insert-member -->
```go
func insertMember(ctx context.Context, tx *database.Tx, id int64) (hooks.Member, error) {
	f := hooks.Members
	return f.Insert(ctx, tx, f.Key().Set(id), f.Name.Set("Ada"), f.Body.Set([]byte{}))
}
```

<!-- orm-example: writes-after -->
```go
func write(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := hooks.Members.Update(ctx, tx, id,
		hooks.Members.Name.Set("Ada"), hooks.Members.Note.SetNull())
	return err
}
```

The examples use the [compiled member declaration](../../tests/fixtures/goway/models/hooks/models.go). A table's `Insert`, `Update` and `Delete` methods retain its declared key type. For additional tenant or authorization predicates, use package `orm.Update` or `orm.Delete` with the query and table key handle. No match returns `database.NotFound`, discoverable through `errors.Is`.

Assignments leave the caller's inputs unchanged. Omission emits no assignment: on insert PostgreSQL supplies its default or NULL; on update the stored value stays unchanged. `Set(0)`, `Set(false)` and `Set("")` are supplied values. A nullable column's `SetNull()` writes SQL NULL. Inside a before-write hook, `orm.WriteUnset` removes an assignment and restores omission.

Required fields without defaults must be supplied. `foundry:"default=database"` permits omission but neither creates nor verifies the SQL default. Migrations own the schema. An omitted `model.ID[M]` primary receives a UUIDv7 unless it has a database default or a distinct mutation input. Natural keys require a supplied value or a database default. An explicitly supplied key is never replaced.

Before hooks can supply missing fields. Managed conventions follow those hooks. Foundry revalidates the effective assignment shape before mutators or encryption, then validates stored values before SQL. Primary-key patches, duplicate/foreign assignments and final empty updates fail. Database defaults and trigger changes are decoded from the returned row; they do not run through Go mutators.

## Visibility and result contracts

`orm.Update` and `orm.Delete` add primary-key equality without removing existing predicates, global scopes or soft-delete visibility. A deletion returns the pre-deletion model for physical deletion, or the stored post-write model for soft deletion. See [soft deletion](model-soft-deletes.md).

Writes reject read ordering, limits, offsets and eager-load options rather than silently dropping them. Inserts also reject explicit query predicates and non-default visibility. A single-model write must return exactly one complete model. Missing, multiple or malformed rows and row-closure errors fail the operation.

[Source writes](model-source-writes.md), [insert-select](model-insert-from-query.md), [upserts](model-upserts.md) and [batch writes](model-batch-writes.md) have explicit result bounds and lifecycle contracts.

## Transaction ownership and failures

Writers implement `database.Transactor`. A normal write owns a transaction, or a savepoint inside the caller's `*database.Tx`. Savepoint success remains contingent on the outer commit. Failure rolls back the write and related transactional hook effects without discarding earlier parent work.

An eligible single insert on a concrete framework pool can use one autocommit `INSERT … RETURNING` when no lifecycle work can run. Complete hydration and clean row closure are required to confirm success. Wrappers retain their transaction ownership. Updates and deletes keep the transaction needed to validate their returned cardinality.

Ordinary failures return no model. If commit confirmation is uncertain, or after-commit work fails, `errors.As` can recover `*orm.WriteError[M]`. Its `Candidate()` retains the hydrated model for reconciliation. Inspect the wrapped `*database.Error` outcome: `Unknown` is not a confirmed rollback; `Committed` means persistence succeeded despite a later failure. Neither a candidate nor an incomplete error inspection proves success. No write is automatically retried.

Framework outcome inspection is bounded to **256 error nodes and 64 wrapping levels**. A custom transaction wrapper can make that inspection incomplete or fail. In that case, a fully hydrated reconciliation candidate may remain available even when its cause has no readable `database.Error` metadata. Callers must never infer rollback, success or permission to retry from the candidate. Reconcile using retained identity and independently established persistence state.

Routine error formatting omits model values and application error text. Use `errors.Is` and `errors.As` for identity and cause inspection. Candidate access is deliberate and can reveal sensitive stored data. When you own the outer transaction, retain reconciliation identity in caller scope and inspect that transaction's final error.

Use the transaction supplied to hooks for durable [outbox](outbox.md) or [audit](audit.md) writes. In-process after-commit callbacks are not crash durable.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
