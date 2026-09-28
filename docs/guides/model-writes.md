# Typed model writes

Generated `Create`, `Update` and `Delete` methods use concrete model drafts and primary keys. Foundry owns parameter binding, one shared mutation compiler, transaction scope and complete `RETURNING` hydration. The [independent PostgreSQL consumer](../../tests/fixtures/consumer/model_write_postgres_test.go) exercises the public API, including database defaults and rollback.

## Creating and updating

```go
q := models.QueryWriteRecords()
record, err := q.Create(ctx, db, models.WriteRecordDraft{}.SetName("first"))
if err != nil {
    return err
}
updated, err := q.Update(ctx, db, record.Code,
    models.WriteRecordDraft{}.SetName("").SetEnabled(false).ClearNote())
```

These calls return complete `models.WriteRecord` values. `record.Code` is the declared `models.RecordKey`, including after fluent query derivation. Other models' drafts and incompatible keys fail Go compilation. Draft construction is immutable: setters return a copy, and `Create` leaves the caller's draft unchanged when preparing an ID.

An omitted draft field emits no assignment. On insert, the database applies its default or NULL; on update, the existing value stays unchanged. Explicit zero, false and empty string are assignments. A nullable `Clear` requests SQL NULL, while `Unset` removes the assignment. The generator exposes no `Clear` for non-nullable fields.

Non-nullable fields are required on create unless tagged `foundry:"default=database"`. This marker permits omission; the SQL default expression belongs exclusively to the migration. It does not create or verify a database default. Explicit values override omission behavior, subject to the database's own identity/generated-column rules. The [fixture model](../../tests/fixtures/consumer/models/write_record.go) uses an identity key and scalar defaults. Other default expressions in tags are rejected.

An omitted `model.ID[ThisModel]` primary key receives a generated UUIDv7 unless tagged with a database-owned default. A supplied ID, including its zero value, is kept and checked by the normal codec/database boundaries. Supply an ID explicitly when application code needs it before persistence. Natural keys without a database default must be supplied.

Models with [automatic field mutators](model-mutators.md) prepare assigned values inside the write transaction, then validate the transformed values before SQL. [Declared hooks](model-hooks.md) run before those transforms and may supply required fields or populate an empty patch. [Managed timestamps](model-timestamps.md) supply the conventional creation/update fields after before hooks and before mutators, including an update timestamp for an otherwise empty patch. Models without hooks, mutators or timestamps retain validation before transaction acquisition. Missing final required fields, invalid codec values, final empty patches and primary-key patches are rejected. Runtime database constraints still apply. SQL casts, column scales, triggers and defaults can alter values; the returned model comes from PostgreSQL's returned row and is decoded through the same codecs used for reads.

## Scoped single-model writes

`Update(ctx, writer, key, draft)` and `Delete(ctx, writer, key)` add a mandatory primary-key equality to the query's existing filters. No match returns `database.NotFound`, discoverable with `errors.Is`. This permits explicit tenant or authorization scopes. Calling code remains responsible for applying the appropriate scope.

`Delete` runs declared hooks and registered observers. Ordinary models return their complete pre-deletion model after physical removal. [Soft-delete models](model-soft-deletes.md) instead update the deletion timestamp and return the stored result; their generated APIs also provide typed `Restore` and `ForceDelete`. Writes reject ordering, limits and nonzero offsets. `Create` also rejects query predicates. These restrictions avoid silently dropping read clauses from a write.

These single-model mutations require exactly one returned row. Missing rows, multiple rows, decoder errors and row-cleanup failures abort the write scope. Partial models are never returned as success. [Typed insertion from queries](model-insert-from-query.md) combines source-owned SQL expressions and literal drafts with explicit set-based lifecycle behavior. [Typed source updates and deletes](model-source-writes.md) add destination-key matching, stored SQL assignments and fixed draft inputs with explicit set-based behavior. [Typed upserts and batch inserts](model-upserts.md) add `Upsert`, `CreateMany` and `UpsertMany` with explicit skipped-row and batch-result contracts through the same compiler and transaction execution.

## Transaction ownership and failures

The writer is the focused `database.Transactor` interface. A pool or session opens a transaction. An existing `*database.Tx` creates a savepoint, letting failed model writes roll back without discarding earlier parent work. Nested success releases the savepoint; the returned model is still contingent on the outer transaction committing. Set isolation/read-only options on that outer transaction.

No write or transaction callback is automatically retried. A normal error returns a zero model. If complete hydration succeeded but commit confirmation is uncertain or after-commit work failed, `errors.As` can recover `*query.WriteError[models.WriteRecord]`. Its `Candidate()` retains the typed returned row, and its wrapped `*database.Error` retains the outcome and original error codes. Inspect `Outcome()`: `Unknown` requires reconciliation; `Committed` means persistence succeeded despite subsequent failure. A candidate alone never proves persistence. The candidate may contain sensitive fields, so ordinary error formatting excludes it.

Custom transaction wrappers receive bounded outcome inspection (256 nodes and 64
wrapping levels). If that inspection is incomplete or fails, a fully hydrated
candidate remains available through `WriteError` for reconciliation. Its cause
may lack readable `database.Error` metadata; do not infer a commit or retry from
the candidate alone. Ordinary known failures still return no candidate.

When the caller owns the outer transaction, inspect the outer transaction's error and retain any needed reconciliation identity in caller scope. An inner model write cannot predict a later outer commit failure. After-commit callbacks are process-local and have no crash durability. See [transaction outcomes](database-runtime.md) for details.

Generated drafts supply typed assignments to `query.Query[M].Insert`, `Patch` and `Remove`, plus the insert/upsert batch execution boundaries. Those shared declaration-level APIs use the same compiler metadata and execution path for every model. Writes also reject explicit eager-loading limits. Declared model hooks and registered observers run automatically on normal writes; durable outbox integration remains milestone 07 work.

## Verification

Compiler tests cover bound SQL, mandatory key scopes, defaults, required fields and invalid assignments. Protocol tests prove rollback on missing/multiple/malformed returned rows, validation before transaction acquisition, nested ownership, uncertain commits and committed after-callback failures. Consumer compile-failure assertions reject draft/key owner mismatches. Real PostgreSQL tests cover generated identities, omitted/zero/null states, exact decimals, filtered updates, uniqueness, deletes of newly created fixture records, malformed returned defaults and savepoint rollback. Test schemas are isolated and retained; no database reset is performed.

[Typed relation writes](model-relation-writes.md) derive pivot keys from existing relationship declarations and preserve normal model lifecycle, typed drafts and transaction outcomes for attach/detach operations.
