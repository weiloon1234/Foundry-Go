# Bounded per-model batch writes

Use `orm.CreateEach`, `orm.UpdateEach`, `orm.DeleteEach`, `orm.RestoreEach` and `orm.ForceDeleteEach` when every model needs its ordinary lifecycle. They return typed model slices and preserve hooks, observers, mutators, conventions and stored changes.

## Create and update models

<!-- orm-example: each-writes -->
```go
func updateMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.UpdateEach(ctx, tx, f.Query().Where(orm.InputTextEq(f.Name, "guest")), 100,
		func(_ context.Context, _ *database.Tx, current hooks.Member) ([]orm.Assignment[hooks.Member], error) {
			return []orm.Assignment[hooks.Member]{orm.InputTextSet(f.Name, "member-"+current.Name)}, nil
		})
}

func createMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.CreateEach(ctx, tx, f.Query(), [][]orm.Assignment[hooks.Member]{
		{orm.KeySet(f.Key(), 1), orm.InputTextSet(f.Name, "first"), orm.ColumnSet(f.Body, []byte{})},
		{orm.KeySet(f.Key(), 2), orm.InputTextSet(f.Name, "second"), orm.ColumnSet(f.Body, []byte{})},
	})
}
```

Creation returns models in input order. Assignment slices remain reusable and unchanged. Before hooks may supply required fields. A valid empty batch returns an empty slice without opening a transaction.

Updates require an explicit positive maximum, at most `orm.MaxPerModelWriteRows`. Foundry locks candidates in primary-key order with one lookahead row, closes the stream and validates the complete candidate set before invoking callbacks. Excess or ambiguous candidates fail rather than silently truncating the batch. Results retain that key order.

The update callback receives the stored model and the operation's actual transaction. Return typed assignments and propagate the supplied context to related work. Original query predicates remain effective for each model. If earlier callbacks make a later candidate ineligible, the operation fails and rolls back. Read ordering, pagination and eager-load clauses are rejected. Internal candidate reads and write hydration skip retrieval callbacks.

## Delete and restore

<!-- orm-example: each-soft-writes -->
```go
func restoreMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.RestoreEach(ctx, tx, f.Query().Where(orm.InputTextEq(f.Name, "guest")), f.Soft, 100)
}

func removeMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.ForceDeleteEach(ctx, tx, f.Query().OnlyTrashed(), f.Soft, 100)
}
```

`DeleteEach` soft-deletes configured models and physically deletes ordinary models. `RestoreEach` selects deleted rows while retaining explicit predicates. `ForceDeleteEach` requires the exact table's `SoftDeletable[M]` token and preserves the query's visibility. `OnlyTrashed()` is appropriate when removing already deleted models.

Ordinary deletion always selects active rows; `OnlyTrashed().DeleteAll` and the equivalent per-model selection cannot delete an already deleted row. Soft deletion returns stored post-write models, restoration returns restored models, and physical deletion returns pre-write models.

## Atomicity and concurrency

One call owns one transaction or one savepoint. Later callback, codec, constraint or SQL failure rolls back earlier models and queued after-commit work. Per-model operations run inside that owner without a savepoint per row. The outer transaction retains locks until commit or rollback.

Cancellation, panic and `runtime.Goexit` follow ordinary lifecycle failure handling. The selected row locks protect existing candidates; they do not claim later inserts or establish serializable predicate locks. There is no hidden retry. A committed callback failure or uncertain commit can retain the candidate slice in `orm.WriteError[[]M]` for reconciliation.

## Set-based writes

<!-- orm-example: set-writes-after -->
```go
func setWrite(ctx context.Context, tx *database.Tx) error {
	_, err := orm.UpdateAll(ctx, tx, orm.WithoutModelHooks(hooks.Members.Query()), orm.InputTextSet(hooks.Members.Name, "guest"))
	return err
}
```

`UpdateAll` and `DeleteAll` return affected counts. `orm.ForceDeleteAll` also requires the soft-delete token. These operations retain filters, global scopes and visibility, but skip per-model hooks, observers and audit. `orm.WithoutModelHooks(q)` acknowledges that choice when callbacks exist or an executor wrapper cannot prove their absence. Per-model operations reject this acknowledgement.

`orm.InsertMany` and `orm.UpsertMany` use assignment batches with explicit set semantics. They never switch to per-model callbacks based on row count. Choose `CreateEach` when those callbacks are required.

## Atomic numeric adjustments

<!-- orm-example: adjustments -->
```go
func addCredit(ctx context.Context, tx *database.Tx) (int64, error) {
	f := models.Orders
	return orm.UpdateAll(ctx, tx, f.Query().Where(orm.NumberLt(f.TotalCents, 1000)), orm.NumberIncrement(f.TotalCents, 100, orm.AtMost[int64](1000)))
}
```

Numeric columns construct `Increment` and `Decrement` assignments for the SQL update. Supply a strictly positive finite magnitude. `AtMost` bounds an increment's result; `AtLeast` bounds a decrement's result. At most one adjustment is accepted per update, with optional ordinary assignments. NULL remains NULL. Database overflow remains a database error.

Per-key adjustments return `database.NotFound` for a missing key or a row hidden by scopes, tenant predicates, soft-delete visibility or other caller predicates. A visible row rejected by the adjustment bound returns `orm.ErrBoundExceeded`, wrapped in `fault.Conflict`. These results are identical with and without local hooks or observers.

When the guarded UPDATE returns no row, or the lifecycle's guarded locking read returns no row, the framework issues **one additional key-only SELECT FOR UPDATE** in the same transaction or nested scope. It removes only the adjustment bound and retains the key, all visibility predicates and locking behavior. The diagnostic read does not hydrate a model or invoke retrieval callbacks. Empty selections fail before write factories or callbacks run on the lifecycle locking-read path. Successful writes add no statement: one UPDATE without lifecycle callbacks, or a locking read followed by UPDATE with them. Query, row-close and cancellation failures remain errors; transaction outcome and reconciliation rules are unchanged. The distinction reflects visibility at the diagnostic read; it does not guarantee that a concurrently deleted row still exists.

Set writes return the number of rows that satisfy the guard, including zero with no error. The legacy API exposed only set-based numeric adjustments, so it had no equivalent per-model adjustment result. Its ordinary guarded per-model patches returned `database.NotFound` both with and without lifecycle callbacks. The [legacy and ORM regression cases](../../database/orm/adjustment_lifecycle_test.go) cover that history and the current contract. Primary keys, mutation-input columns and managed timestamps cannot be adjustment targets. See [typed declaration roles](model-declarations.md).

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
