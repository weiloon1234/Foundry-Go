# Typed conflicts and batch inserts

`orm.Upsert` and `orm.UpsertMany` combine insertion with an explicit conflict policy. `orm.InsertMany` inserts a batch without a conflict action. All use typed assignments, codecs, complete returned models and ordinary transaction outcomes.

## Select conflict behavior

<!-- orm-example: upsert-user -->
```go
func upsertUser(ctx context.Context, tx *database.Tx) (value.Optional[models.User], error) {
	f := models.Users
	policy := orm.OnConflict(orm.ConflictKey(orm.TextExpr(f.Email))).DoUpdate(orm.ConflictIncoming(orm.NumberExpr(f.Age)))
	return orm.Upsert(ctx, tx, f.Query(), policy,
		orm.TextSet(f.Email, "ada@example.test"), orm.NumberSet(f.Age, 30),
		orm.EnumSet(f.Status, models.StatusActive), orm.EnumSet(f.Level, models.LevelBasic))
}
```

The migration must provide a matching unique index. `orm.ConflictKey` retains model ownership, including composite targets with different value types. `DoUpdate` takes `ConflictIncoming` to copy the proposed stored column, `ConflictSet` for a fresh typed input or `ConflictNull` for NULL. Primary-key updates are rejected.

`orm.OnConflict[Model]().DoNothing()` skips any eligible unique conflict. A field target's `DoNothing` skips only that target. `orm.OnConflictConstraint[Model](name)` is an identifier-validated named-constraint boundary; PostgreSQL checks its existence. Empty targets cannot request an update.

A single upsert returns `value.Optional[M]`. Absence means `DO NOTHING` or a false update condition skipped the result. It never invents the existing row or identifies insertion versus update. Explicit query predicates are invalid on an insert destination; put update conditions on the policy's `Where` or `rows.ConflictWhereRows`.

Active destination scopes join conflict update conditions. An unconditional update that skips a row outside those scopes fails with `fault.Conflict`; a user-supplied condition retains its ordinary skipped-result meaning. This prevents a globally unique key from bypassing tenant filtering during update. Applications still authorize supplied insert values.

## Calculating conflict updates

<!-- orm-example: excluded-after -->
```go
func excluded() orm.Conflict[models.User] {
	r := rows.ConflictRowsOf(models.Users.Query())
	stored := expr.At(r.Stored(), orm.NumberExpr(models.Users.Age))
	proposed := expr.At(r.Proposed(), orm.NumberExpr(models.Users.Age))
	return orm.OnConflict(orm.ConflictKey(orm.TextExpr(models.Users.Email))).DoUpdate(
		rows.SetConflictValue(orm.NumberExpr(models.Users.Age), expr.Add(stored, proposed)))
}
```

`rows.ConflictRowsOf` supplies stored and proposed scopes. `expr.At` binds columns to those scopes; `rows.SetConflictValue` requires the destination's exact stored type. The proposed row includes database defaults and `BEFORE INSERT` trigger changes. All calculations see the same records, independent of assignment order.

An explicitly selected incoming column copies its proposed default or NULL even when omitted from inputs. Managed update time copies its proposed value automatically. Creation time changes on conflict only when selected. Input columns accept literal transformations and own-field incoming values, but reject arbitrary SQL calculations that would bypass their Go mutator. Encrypted conflict-update targets are rejected.

`rows.ConflictWhereRows` can compare stored and proposed values. `ConflictScalarRowQuery` and `ConflictScalarNullableRowQuery` adapt independent scalar queries; missing scalar rows remain NULL. Correlated expressions retain their own typed scopes and shared CTE/alias planner. Multiple scalar rows fail through PostgreSQL's cardinality rules.

## Expression and partial-index targets

`rows.OnConflictKeys` accepts model-owned `expr.Group` keys, including expressions such as lowercase text. `TargetWhere` accepts model predicates; `rows.ConflictTargetWhere` accepts expression predicates. These describe index inference, not a filter on input rows. Update conditions belong to `Where` or `ConflictWhereRows`.

Target constants are fixed schema values rendered as escaped SQL literals after codec validation. Never put request secrets in index declarations: explicit SQL inspection can reveal those constants. Insert values and update conditions remain bound parameters. Named constraints and catch-all targets cannot have partial-index predicates. Aggregates, windows, subqueries, hidden EXISTS, duplicate keys and foreign fields are rejected as targets.

## Atomic batches and outcomes

`InsertMany` and `UpsertMany` accept `[][]orm.Assignment[M]`. One call is bounded by `orm.MaxInsertRows` and the statement-wide `orm.MaxParameters` parameter/cell limit; wide rows can require smaller batches. See the [consumer resource limits](../../database/orm/README.md#consumer-resource-limits). It is atomic; a failed row, malformed return, codec error or duplicate conflict target rolls back the batch. Empty batches still validate declarations and policies without executing SQL or mutators.

Batches return only complete inserted/updated rows. Skipped rows are absent, and PostgreSQL return order is not an input mapping. Do not zip results to inputs. Use stable domain keys to associate results.

These insert/upsert operations explicitly skip per-model write hooks, observers, audit and retrieval hooks. They do not require a separate hook acknowledgement. Mutators and managed conventions still run. Choose `CreateEach` for per-model behavior.

No automatic retries occur. An uncertain commit or committed callback failure can retain typed results through `orm.WriteError[R]`; a candidate is not proof of persistence. See [write outcomes](model-writes.md#transaction-ownership-and-failures).

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
