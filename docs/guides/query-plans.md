# Typed PostgreSQL query plans

`orm.Explain(ctx, executor, q)` asks PostgreSQL for a plan without executing the selected statement. `orm.ExplainAnalyze` executes the SELECT and reports actual measurements. Both preserve compiled SQL, bindings, context scopes and executor ownership. They skip model hydration, retrieval hooks and eager loading.

<!-- orm-example: explain -->
```go
func inspectOrders(ctx context.Context, db database.Executor) (orm.Plan, error) {
	return orm.Explain(ctx, db, models.Orders.Query().Where(models.Orders.TotalCents.Gt(100)))
}

func analyzeOrders(ctx context.Context, tx *database.Tx) (orm.Plan, error) {
	return orm.ExplainAnalyzeLocked(ctx, tx, models.Orders.Query().ForUpdate())
}
```

## Transaction ownership

Use `ExplainLocked` or `ExplainAnalyzeLocked` for an ORM locked query. Both require `*database.Tx`. Analysis acquires the selected locks in that transaction; commit or rollback remains the caller's responsibility. A SELECT can invoke volatile functions, so treat analysis as execution. There is no arbitrary-statement shortcut.

For projection/scalar/set results adapted through `rows.Read`, `ReadScalar` or `ReadSet`, use `rows.Explain` and `rows.ExplainAnalyze`. The rows package also exposes locked, transaction-record and cursor variants. Cursor inspection plans the canonical completed query without inventing a pagination boundary.

Compilation failures are wrapped safely before callback isolation can format application errors. `errors.Is` and `errors.As` retain causes. Scope failures do not become unscoped plans. Cancellation and executor errors return no partial successful plan.

## Inspect a plan

An `orm.Plan` owns bounded PostgreSQL plan data. `Root()` returns a typed tree snapshot; `NodeCount()` and `Analyzed()` describe it. `PlanningMilliseconds()` and `ExecutionMilliseconds()` return optional measurements. Costs use PostgreSQL planner units, while times use milliseconds. Actual statistics are absent on non-analyzed plans. Zero estimates are valid; missing or NULL required costs are invalid.

`JSON()` returns an owned copy of the full response, including PostgreSQL-specific fields. It and the tree can reveal filters or bound values. Expose them only through an authorized inspection path. Routine plan formatting prints a summary without those contents.

Responses are bounded to 4 MiB, 4,096 nodes and 24 tree levels. Malformed, duplicate-key or inconsistent-mode responses fail. The [PostgreSQL contract tests](../../database/query/explain_postgres_test.go) cover bindings, side effects, savepoints, locks and deadlines.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
