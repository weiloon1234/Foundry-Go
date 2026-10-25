# Update and delete using typed sources

`rows.UpdateFrom`, `rows.DeleteUsing` and `rows.ForceDeleteUsing` use query sources to select matching destination models. Sources can retain typed aliases, joins, CTEs, filters and windows. These operations are explicitly set-based and skip per-model lifecycle callbacks.

## Match identities and map stored values

<!-- orm-example: source-update -->
```go
func updateMemberBodies(ctx context.Context, tx *database.Tx) (int64, error) {
	f := hooks.Members
	destination := f.Query()
	return rows.UpdateFrom(destination, rows.From(f.Query())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).
		Select(rows.MapUpdate(f.Body, expr.Value(f.Body))).
		Values(f.Name.Set("updated")).Exec(ctx, tx)
}
```

`rows.MatchSource` matches a stored expression to the destination primary key without assigning it. `MatchNullableSource` permits nullable source keys, such as an outer join; SQL NULL matches nothing. A key match is a distinct type from an assignment and cannot be converted into one.

`rows.MapUpdate` requires the exact stored value and nullable shape and an `orm.UpdateTarget`. Primary keys, managed update timestamps and mutation-input fields cannot be targets. Adapters cannot erase those roles. SQL mappings do not call Go getters or transformations.

Destination predicates, scopes and soft-delete visibility stay effective. Source windows belong to the source; read ordering, limits and eager-load options on the destination are rejected.

## Fixed values and duplicate matches

`Values(assignments...)` replaces literal inputs applied once per attempt to every affected model. Mutators and managed conventions run inside the transaction. Fixed inputs and SQL mappings cannot overlap, repeat or patch the primary key. Use per-model batches for Go logic that must run once per selected model.

An SQL-mapped update with multiple source matches for one affected destination fails with `database.TooManyRows` and rolls back. It never selects an arbitrary first source value. The complete source window is materialized and matches use the database's key comparison. Fixed-only updates can accept repeated matches because each destination receives the same inputs once. Deletes also affect each matching destination once.

## Delete using a source

<!-- orm-example: source-delete -->
```go
func deleteMatchingMembers(ctx context.Context, tx *database.Tx) (int64, error) {
	f := hooks.Members
	destination := f.Query().OnlyTrashed()
	return rows.ForceDeleteUsing(destination, f.Soft, rows.From(f.Query().OnlyTrashed())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).Exec(ctx, tx)
}
```

Ordinary `DeleteUsing` soft-deletes configured models and physically deletes ordinary models. `ForceDeleteUsing` requires the declared `SoftDeletable[M]` token and preserves destination visibility. Use `WithTrashed` or `OnlyTrashed` to include deleted models. Physical deletion returns complete pre-deletion models.

## Results and ownership

`Exec` returns a count without collecting models. `Returning` requires an explicit positive bound, at most 1,000 models. Overflow rolls back rather than truncating. Results have no promised order. Empty matches return zero or an empty slice; destination-excluded and trigger-suppressed rows do not count as affected.

These source writes skip write hooks, observers, audit and retrieval callbacks by their explicit contract, without separate acknowledgement. Source and destination scopes still apply. Transactions/savepoints preserve rollback on failure and cancellation; no hidden retry occurs. `orm.WriteError[R]` retains a complete candidate only for outcome reconciliation, never as proof of commit. See [write outcomes](model-writes.md#transaction-ownership-and-failures).

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
