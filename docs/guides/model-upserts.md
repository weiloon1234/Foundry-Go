# Typed upserts and batch inserts

Generated model queries expose `Upsert`, `CreateMany` and `UpsertMany`. They use handwritten model structs, generated drafts and field descriptors through the same insert compiler, codecs and transaction runtime as `Create`. The [consumer acceptance](../../tests/fixtures/consumer/upsertqueries/upserts_postgres_test.go) exercises these APIs against PostgreSQL.

## Selecting conflict behavior

```go
u := models.UserFields()
policy := query.OnConflict(u.Email, u.Level).Update(u.Age)

result, err := models.QueryUsers().Upsert(ctx, db,
    models.UserDraft{}.
        SetEmail("person@example.test").
        SetAge(30).
        SetStatus(models.StatusActive).
        SetLevel(models.LevelBasic),
    policy)
```

The example's migration owns a unique index on email and level. Go checks model ownership and value types; PostgreSQL checks whether the physical index matches the declared conflict target. Composite targets can contain fields of different types. Model-owned UUIDs and natural keys retain their usual draft/default behavior.

`Upsert` returns `(value.Optional[User], error)`. A present result is a completely hydrated inserted or updated model. An omitted result means the policy skipped that input. It never fabricates the existing model after `DO NOTHING` or reports whether a returned row was inserted versus updated.

A model's active [global scopes](model-global-scopes.md) join the `DO UPDATE` condition, so a conflicting row outside them (another tenant's, say) is never changed. An update without its own `Where`/`WhereRows` condition that skips such a row fails with `fault.Conflict` rather than returning an omitted result; the insert rolls back with it.

`Update(fields...)` copies those fields from PostgreSQL's proposed `EXCLUDED` row. Other stored fields remain unchanged. An explicitly selected incoming field that was omitted from the draft copies its database default or NULL. This makes the selected update list significant; omission alone does not preserve a field you explicitly selected for incoming replacement.

Models with [managed timestamps](model-timestamps.md) always copy their proposed `UpdatedAt` on an update action, replacing an explicit assignment for that field. The proposed value has already passed its Go mutator; it is not transformed twice. The convention does not automatically replace `CreatedAt` on conflict.

For typed constant values, nullable clearing and update conditions:

```go
policy := query.OnConflict(u.Email, u.Level).
    DoUpdate(u.Age.Incoming(), u.Status.Set(models.StatusDisabled),
        u.Nickname.SetNull()).
    Where(u.Status.Eq(models.StatusActive))
```

`Where` evaluates the existing conflicting row. A false condition returns no model even though PostgreSQL may lock the row while evaluating the conflict. These policies preserve their original values when derived. Field `Set`/`SetNull` here construct conflict assignments; ordinary model mutations use generated drafts.

Use `query.OnConflict(u.Email, u.Level).DoNothing()` to skip that target, or `query.OnConflict[models.User]().DoNothing()` to skip every eligible unique conflict. `query.OnConflictConstraint[models.User]("users_email_level_key")` is an explicit, identifier-validated boundary for a named constraint. Prefer generated fields where index inference expresses the target. PostgreSQL documents [conflict arbitration, proposed values and returned rows](https://www.postgresql.org/docs/18/sql-insert.html#SQL-ON-CONFLICT).

No query filter can silently become an upsert condition: `QueryUsers().Where(...).Upsert(...)` is invalid. Place an intended update condition on the conflict policy. Applications still own authorization and which insert values/targets a caller may use.

## Expression and partial-index targets

Use row expression keys when the migration's unique index contains a calculation:

```go
u := models.UserFields()
policy := query.OnConflictKeys(query.Lower(u.Email).Group(), u.Level.Group()).
    TargetWhere(u.Status.Eq(models.StatusActive)).
    Update(u.Age)
```

This target corresponds to a unique index on `lower(email_address), level` with the predicate `status = 'active'`. `Group()` supplies a model-owned row key; it does not group the insert. Expression keys, ordinary fields and predicates all retain their model owner. Selected aggregate/window keys cannot enter this API. Runtime validation rejects scalar subqueries, hidden EXISTS queries, duplicate keys and undeclared fields before executing a write, including for empty batches.

`TargetWhere` describes which index PostgreSQL may infer. It does not filter incoming rows or decide whether to update a found conflict. Use `Where` or `WhereRows` for the latter. Rows outside a partial index may insert independently; PostgreSQL may also infer a matching non-partial index. Named constraints and catch-all conflict targets cannot carry `TargetWhere`.

Declare target constants from the migration's fixed schema values. Foundry validates them through their codecs and renders escaped SQL constants, so index inference works with generic prepared plans. Those constants are visible in `Statement.SQL()` and its formatting; do not use request values or secrets as index declarations. Insert values, update calculations and update conditions remain bound parameters. Text escaping preserves quotes and backslashes with either PostgreSQL string-literal setting; binary and temporal constants retain their database representation.

Migrations own the physical indexes. Expression operator types and casts must match them, and PostgreSQL enforces function immutability and index inference. An unmatched index returns a database error and rolls back the write scope. See the [independent consumer examples](../../tests/fixtures/consumer/upsertqueries/index_targets_postgres_test.go) and PostgreSQL's [index inference](https://www.postgresql.org/docs/18/sql-insert.html#SQL-ON-CONFLICT) and [partial-index planning](https://www.postgresql.org/docs/18/indexes-partial.html) documentation.

## Calculating conflict updates

Use the model query's conflict scope with the same generated field sets used by aliases and joins:

```go
q := models.QueryWriteRecords()
f := models.WriteRecordFields()
rows := q.ConflictRows()
stored := models.WriteRecordFieldsAt(rows.Stored())
proposed := models.WriteRecordFieldsAt(rows.Proposed())

policy := query.OnConflict(f.Name).DoUpdate(
    query.SetConflictValue(f.Amount, query.Add(stored.Amount, proposed.Amount)),
    query.SetConflictValue(f.Note, query.CoalesceNullable(proposed.Note, stored.Note)),
).WhereRows(proposed.Enabled.Eq(true))
```

This consumer model declares `Amount` as an exact `decimal.Decimal` and `Note` as `value.Nullable[string]`. The calculation adds the stored amount to the proposed amount, and keeps the stored note when the proposed note is NULL. `query.SetConflictValue` accepts the field's exact type in `ConflictRow[Model]`; unrelated models, ordinary model-scope values and selected aggregate/window expressions cannot be assigned accidentally. Nullable destinations accept nullable expressions; `query.NullableRow` explicitly widens a non-null expression.

`Proposed` includes database defaults and `BEFORE INSERT` trigger changes. Every calculation reads the same existing/proposed records: assignment order does not create a sequence of intermediate models. `WhereRows` can compare both records using ordinary typed predicates, including `query.Greater`, `query.Equal`, CASE and JSON-path expressions. Existing `Where` continues to inspect only the stored model. All added conditions are combined with AND. Query filters used when obtaining `ConflictRows` do not become conflict predicates.

The scope cannot execute an ordinary SELECT. For an independent scalar query, use `query.ConflictScalarRowQuery(rows, selected)` or `query.ConflictScalarNullableRowQuery` for an already nullable result. Empty scalar results remain NULL, so use a typed fallback before assigning a non-null field. For correlation, use `query.Correlate(rows, innerSource)`, bring `rows.Stored()`/`rows.Proposed()` into that scope with `query.OuterScope`, and wrap the selection with `query.CorrelatedScalarRowQuery` or its nullable counterpart. Inner queries retain their own grouping/window phase; multiple scalar rows remain a PostgreSQL cardinality error and roll back the write scope.

Assignment subqueries and conditions share the statement's CTE planner, alias allocation, codec binding and resource bounds. Use distinct aliases when an inner query would shadow a conflict record (notably the proposed record named `excluded`). Normal values remain bound parameters. The [calculation acceptance fixture](../../tests/fixtures/consumer/upsertqueries/calculations_postgres_test.go) contains complete consumer examples.

## Atomic batches

```go
created, err := models.QueryCountries().CreateMany(ctx, db,
    []models.CountryDraft{
        models.CountryDraft{}.SetCode(models.CountryCode("MY")).SetName("Malaysia"),
        models.CountryDraft{}.SetCode(models.CountryCode("SG")).SetName("Singapore"),
    })

updated, err := models.QueryCountries().UpsertMany(ctx, db, drafts,
    query.OnConflict(models.CountryFields().Code).
        Update(models.CountryFields().Name))
```

Here `drafts` is `[]models.CountryDraft`. Results are ordinary `[]models.Country`. One bounded batch produces one SQL insert/upsert statement inside the write transaction. A pool or session opens a transaction; an existing transaction supplies a savepoint. No per-row query loop or automatic retry is hidden inside the batch API.

Each draft retains its own omitted/zero/NULL states. The compiler emits a deterministic column union and uses SQL `DEFAULT` for omitted cells. It supports batches whose rows all use database defaults. UUID preparation leaves the original drafts unchanged. Every non-nullable, non-defaulted field must be supplied for each input row before execution.

A batch may contain at most `query.MaxInsertRows` inputs, with additional shared cell and parameter bounds. A valid empty batch returns an empty slice without opening a transaction; invalid queries or policies still fail. Callers choose whether to split larger workloads and whether those batches share an outer transaction.

Returned order follows PostgreSQL's `RETURNING` stream and is not an input-position mapping. Skipped conflicts contribute no result; correlate returned models by their declared keys. Repeated input rows targeting the same stored row with `DO UPDATE` are a PostgreSQL cardinality error. The whole batch rolls back, preserving that error through the database runtime.

## Validation and transaction outcomes

Conflict targets and update assignments reject other model owners at Go compilation. Setters require the field's concrete value type, and only nullable fields expose `SetNull`. Runtime checks reject missing actions, malformed named constraints, repeated or undeclared columns, primary-key updates, empty update lists, incompatible read clauses, and invalid codec values before SQL. Models with field mutators prepare and validate inside the transaction; other models retain validation before transaction acquisition.

`CreateMany` requires one complete returned row per input. Upserts permit skipped rows but reject excess rows. Any decoding or row-cleanup error discards the result and rolls back the write scope, including rows decoded successfully earlier in the batch. An outer transaction still controls final persistence after a successful nested write.

The [write outcome contract](model-writes.md#transaction-ownership-and-failures) applies to these result shapes. When commit confirmation is uncertain or after-commit work fails, `query.WriteError[R]` preserves a complete candidate of the terminal's result type: `value.Optional[User]` for `Upsert`, or `[]User` for batch calls. Inspect the wrapped database outcome before reconciliation. Error formatting excludes candidate fields. Returned candidates retain ordinary Go value/reference semantics.

These SQL upsert and batch APIs have explicit set-based lifecycle semantics, including one-input `Upsert`. Assigned draft inputs and literal conflict assignments run [automatic field mutators](model-mutators.md); copying a field's own proposed value does not transform it again. Fields with Go mutators reject other SQL conflict calculations and cross-field copies before execution, preventing normalization bypass. Fields without mutators retain their SQL expression support. These set-based methods run no model observers. Use [per-model batch writes](model-batch-writes.md) when each input or selected model needs its ordinary lifecycle. [Lifecycle-aware lookup writes](model-lookup-writes.md) provide `FirstOrCreate` and `UpdateOrCreate`; concurrent absence relies on physical uniqueness and reports ordinary conflicts without hidden retries. Physical indexes and constraints remain owned by migrations.

## Verification

Compiler and protocol tests cover defaults, quoted aliases/constraints, constant and incoming assignments, NULLs, immutable policies, bounds, conditional subqueries/CTEs, empty batches, failed hydration and typed reconciliation candidates. PostgreSQL acceptance covers mixed conflict outcomes, UUID/composite/natural keys, concurrent upserts sharing one identity, a physical table named `excluded`, duplicate-target rollback and recovery after a malformed returned decimal. Consumer negative-compilation and gopls checks cover the public model/value boundaries.

[Typed insertion from queries](model-insert-from-query.md) selects stored SQL values into another model. Its literal draft uses ordinary setters, while generated SQL selectors exclude mutated columns and managed update timestamps. Choose an affected count or explicitly bounded complete-model results.
