# Lifecycle-aware lookup writes

`orm.FirstOrCreate`, `orm.CreateOrFirst` and `orm.UpdateOrCreate` are package functions over a typed model query. They preserve the ordinary per-model lifecycle and return complete stored models.

## Find first or create

<!-- orm-example: first-or-create-user -->
```go
func firstOrCreateUser(ctx context.Context, tx *database.Tx) (models.User, error) {
	f := models.Users
	return orm.FirstOrCreate(ctx, tx, f.Query().Where(orm.TextEq(f.Email, "ada@example.test")),
		orm.NumberSet(f.Age, 30), orm.EnumSet(f.Status, models.StatusActive), orm.EnumSet(f.Level, models.LevelBasic))
}
```

An existing match is selected in primary-key ascending order without preparing creation inputs or invoking write hooks. Multiple matches do not prove uniqueness; the first is selected. Write-owned lookups skip retrieval callbacks.

On creation, omitted fields default from supported top-level equality predicates, including active scopes resolved with this context. Explicit assignments win. Ranges, OR, IN, conflicting equalities and Go-mutated fields supply no default: a stored value is not a mutator input. Hooks may still supply required fields.

After creation, the stored identity must satisfy the original predicates and visibility. A default, normalization or hook that moves it outside those conditions causes rollback. This postcondition does not create a unique index.

## Create first or update an existing model

<!-- orm-example: lookup-branches -->
```go
func createOrFindUser(ctx context.Context, tx *database.Tx) (models.User, error) {
	f := models.Users
	return orm.CreateOrFirst(ctx, tx, f.Query().Where(orm.TextEq(f.Email, "ada@example.test")), orm.NumberSet(f.Age, 30), orm.EnumSet(f.Status, models.StatusActive), orm.EnumSet(f.Level, models.LevelBasic))
}

func updateOrCreateUser(ctx context.Context, tx *database.Tx) (models.User, error) {
	f := models.Users
	return orm.UpdateOrCreate(ctx, tx, f.Query().Where(orm.TextEq(f.Email, "ada@example.test")),
		[]orm.Assignment[models.User]{orm.NumberSet(f.Age, 30), orm.EnumSet(f.Status, models.StatusActive), orm.EnumSet(f.Level, models.LevelBasic)},
		func(context.Context, *database.Tx, models.User) ([]orm.Assignment[models.User], error) {
			return []orm.Assignment[models.User]{orm.NumberSet(f.Age, 31)}, nil
		})
}
```

`CreateOrFirst` attempts ordinary insertion in an isolated savepoint. It rolls that savepoint back and looks up a competitor only for a server-reported unique violation proven to belong to the destination insert. Schema, table and constraint provenance must all match the resolved destination. Hook errors, trigger writes to other tables or schemas, missing metadata and claimed application errors do not enter that recovery path.

If the competitor is not visible under the original query, the unique violation remains an error. This relies on READ COMMITTED visibility; a competitor committed after a REPEATABLE READ/SERIALIZABLE snapshot may remain invisible. Unknown outcomes never become success, definite rollback or an automatic retry.

Conflict recovery requires the server-reported schema, table and constraint to match a unique index of the destination. Unqualified destinations resolve through the transaction's `search_path`. Missing or mismatched identity, including a same-named index in another schema or a partition or inheritance child violation, preserves the original error. Deferred constraint failures at commit do not trigger a competitor lookup or retry.


`UpdateOrCreate` prepares only the chosen branch. The existing model is locked and rows close before its callback. The callback receives the actual transaction and returns assignments. Creation-only keys stay out of the update. The subsequent ordinary update preserves original predicates and the selected key; changed values may leave the old filter, as with any update.

## Ownership and concurrency

Lookups retain scopes and soft-delete visibility, reject read windows/eager-load clauses, and never silently restore hidden rows. A hidden deleted row may still own a unique key. Select `WithTrashed` explicitly when that is intended.

Related callback work uses the supplied context and transaction. Callback errors leave the boundary as safe lifecycle failures with `errors.Is`/`errors.As` identity. Hook, codec, constraint, cancellation and postcondition failures roll back the helper's changes and queued after-commit work.

An absent lookup does not lock absence. `FirstOrCreate` and `UpdateOrCreate` can race to insert; physical uniqueness decides the winner. `CreateOrFirst` has the restricted conflict recovery above, not a general retry loop. Inspect `orm.WriteError[M]` and the outer transaction outcome before reconciliation. See [transaction ownership](model-writes.md#transaction-ownership-and-failures).

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
