# Soft deletion and restoration

Declare `value.Nullable[time.Time]` or `value.Nullable[temporal.DateTime]` through `orm.SoftDeleteOf`. Store the resulting `SoftDelete[M, V]` column and `orm.SoftDeletes(column)` token in the table declaration. The [typed roles guide](model-declarations.md) shows `Users.DeletedAt` and `Users.Soft`.

The column must be non-primary. Migrations own its nullable SQL column. `NoSoftDeletes()` leaves an ordinary deletion field under application control and conflicts with an explicit `SoftDeleteOf` role. A nullable timestamp declaration alone does not supply the force-delete capability.

## Typed writes and visibility

`Table.Delete` and package `orm.Delete` soft-delete configured models, returning the stored post-write model. `Table.Restore` clears the deletion time on one deleted key. `orm.RestoreEach` restores a bounded set and requires the soft token. Ordinary models retain physical deletion.

<!-- orm-example: force-after -->
```go
func forceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := orm.ForceDelete(ctx, tx, Users.Query().WithTrashed(), Users.Soft, Users.Key(), id)
	return err
}
```

Package `orm.ForceDelete` accepts a query, soft token, typed key handle and stored key. `Table.ForceDelete` also requires the token. Both physically remove the model and return its pre-write snapshot. No match returns `database.NotFound`. Zero and foreign-declaration tokens fail before SQL.

Default queries select active rows. `WithTrashed()` includes both active and deleted rows, `OnlyTrashed()` selects deleted rows and `WithoutTrashed()` restores default visibility. These immutable choices preserve explicit predicates and global scopes.

Ordinary delete always selects active rows. Restore selects deleted rows. Force deletion preserves the current query visibility: include trashed rows explicitly when removing already deleted models. Visibility applies to reads, counts, aggregates, relations, source queries and writes. Insert rejects non-default destination visibility.

## Relations and query composition

Targets and pivots each retain their own visibility. A parent's `WithTrashed` does not expose deleted children. Relation descriptors support target `WithTrashed`, `OnlyTrashed` and `WithoutTrashed`; through descriptors separately support the pivot variants ending in `Pivot`.

Joins, subqueries and CTEs keep visibility inside each model source. A left join to a hidden row retains the unmatched outer row. Soft deletion does not cascade, release a unique key or change foreign-key semantics. Migrations must declare any partial unique index needed by the domain.

## Lifecycle, time and failures

| Operation | Callback order around SQL |
| --- | --- |
| Soft delete | `Deleting`, conventions, preparation, `UPDATE`, `Deleted` |
| Restore | `Restoring`, conventions, preparation, `UPDATE`, `Restored` |
| Force delete | `ForceDeleting`, `Deleting`, `DELETE`, `Deleted`, `ForceDeleted` |

Local hooks precede observers within each stage. These operations do not also emit saving/updating stages. Before callbacks receive the locked stored model; after callbacks receive `orm.Changes[M]` with the operation kind. Soft deletion/restoration retain both snapshots; physical deletion has no after-model.

Soft deletion and managed update time share one clock sample. Restoration clears deletion time and updates managed update time. Without managed update time, restoration needs no sample. Force deletion updates neither timestamp.

Ordinary assignments to the deletion field remain create/update operations. They do not dispatch deletion or restoration callbacks. Hook errors, cancellation, invalid values and SQL failures roll back the operation or savepoint. A unique-conflict restore leaves the model deleted. After-commit callbacks follow the [normal transaction contract](model-hooks.md#transactions-and-ownership).

[Per-model batches](model-batch-writes.md) preserve lifecycle and audit. Set-based operations skip those callbacks, and upsert visibility does not silently become a restoration action.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
