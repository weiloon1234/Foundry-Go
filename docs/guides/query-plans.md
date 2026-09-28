# Typed PostgreSQL query plans

Typed plans passed milestone 23 native PostgreSQL, race and fuzz verification. The
[consumer functions](../../tests/fixtures/consumer/tooling/database.go) show both
ordinary planning and transaction-bound execution analysis using public APIs.

`query.Explain(ctx, executor)` asks PostgreSQL for a JSON plan without executing
the selected statement. `query.ExplainAnalyze(ctx, executor)` explicitly executes
the SELECT and reports actual rows, loops and elapsed measurements. Both reuse
the exact compiled SQL and parameter bindings, selected executor and context.
They do not hydrate models, invoke retrieval hooks or load eager relations.

These methods exist on model, projection, set, value and cursor result queries.
Cursor inspection plans its canonical result without inventing a pagination
boundary. Locked and transaction-constrained queries require `*database.Tx` even
for ordinary planning. Analysis acquires the SELECT's row locks in that actual
transaction; its commit/rollback remains the caller's responsibility. Current
query ASTs describe SELECTs; there is no arbitrary statement/executor shortcut.

Execution analysis can run volatile functions called by a SELECT. Treat it as
execution and choose the transaction deliberately. Ordinary planning does not
execute the selected statement. The [PostgreSQL tests](../../database/query/explain_postgres_test.go)
check preserved bindings, side effects, savepoint rollback, locks and deadlines.

A `query.Plan` owns its response. `Root()` returns a typed tree snapshot;
`NodeCount()` and `Analyzed()` describe it. `PlanningMilliseconds()` and
`ExecutionMilliseconds()` return optional measurements. Node costs use PostgreSQL
planner units; time measurements use milliseconds. Actual statistics are absent
on non-analyzed plans. Zero estimates are valid; missing/null required costs are
rejected as malformed data.

`JSON()` returns an owned copy retaining PostgreSQL-specific fields outside the
common typed tree. It can include filters and bound values: expose it only
through an explicitly authorized inspection path. Routine formatting of `Plan`
prints a summary without its contents. Responses are bounded to 4 MiB, 4,096 plan
nodes and 24 tree levels; malformed, duplicate-key or inconsistent-mode documents
return an error, never a partial successful plan.
