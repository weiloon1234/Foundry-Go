# Typed common table expressions

`query.CTE(name, query)` declares a reusable, read-only common table expression from a complete model or projection query. It preserves the record's Go type. Use `As[Tag]` to give each reference its own typed scope, then compose [joins](model-joins.md), [projections](model-projections.md) and [subqueries](model-subqueries.md) through the existing APIs. The [independent consumer fixture](../../tests/fixtures/consumer/ctes/ctes_postgres_test.go) exercises these examples against PostgreSQL.

## Filter models with a CTE

```go
type eligibleAlias struct{}

u := models.UserFields()
eligible := query.CTE("eligible_users",
    models.QueryUsers().Where(u.Age.Gte(21)),
)
source := query.As[eligibleAlias](eligible, "eligible")
fields := models.UserFieldsAt(source.Scope())
ids := query.SelectValue(source, fields.ID.Value())

users, err := models.QueryUsers().Where(u.ID.InQuery(ids)).All(ctx, db)
```

`eligible` is a `CommonTable[models.User]`, and `ids` produces `model.ID[models.User]` values. An order ID cannot replace a user ID. `users` is a slice of complete `models.User` values. No database call occurs until `All`; `First` and `RequireFirst` retain the [usual optional/required result contracts](model-queries.md).

CTEs do not expose model writes or execute by themselves. The outer model builder remains available for typed `Find`, `Update` and `Delete`, with its CTE predicate constraining the operation. Existing transaction, cancellation and error behavior applies. CTE-based filters also work inside explicit correlations, relationship existence filters, eager-loading descriptors and relation aggregates.

## Reuse a definition

```go
type firstAlias struct{}
type secondAlias struct{}

eligible := query.CTE("eligible_users",
    models.QueryUsers().Where(models.UserFields().Age.Gte(21)),
).Materialized()
first := query.As[firstAlias](eligible, "first_user")
second := query.As[secondAlias](eligible, "second_user")
a, b := models.UserFieldsAt(first.Scope()), models.UserFieldsAt(second.Scope())
joined := query.InnerJoin(first, second, query.On(a.ID, b.ID))
fields := models.UserFieldsAt(query.LeftScope(joined, first.Scope()))
emails, err := query.SelectValue(joined, fields.Email.Value()).All(ctx, db)
```

References carry their definition. Foundry emits each reused descriptor once and places dependencies before their consumers. A definition can itself consume another CTE, including through nested projections and subqueries. There is no separate string-based registration list.

Derive materialization options before creating references. `Materialized()` and `NotMaterialized()` return new descriptors without changing captured queries. Reusing one descriptor shares a definition; creating different definitions with the same SQL name is an error, including conflicting materialization choices. Give unrelated definitions distinct names.

The default leaves materialization to PostgreSQL. Explicit materialization can avoid repeated evaluation, while `NotMaterialized` can allow predicate pushdown and repeated evaluation. Choose from measured query behavior; neither option is a persistent application cache. [PostgreSQL materialization semantics](https://www.postgresql.org/docs/18/queries-with.html#QUERIES-WITH-CTE-MATERIALIZATION)

## Use declared report records

```go
type totalsAlias struct{}
o := models.OrderFields()
totals := reports.ProjectBuyerTotals(models.QueryOrders()).
    SelectBuyerID(o.BuyerID.Value()).
    SelectTotal(o.TotalCents.Sum().Value()).
    SelectOrders(query.Count[models.Order]().Value()).
    SelectAverage(o.TotalCents.Avg().Value()).
    Query().GroupBy(o.BuyerID.Group()).
    Having(query.Count[models.Order]().Gte(2)).Limit(1)

source := query.As[totalsAlias](query.CTE("buyer_totals", totals), "totals")
fields := reports.BuyerTotalsFieldsAt(source.Scope())
amounts, err := query.SelectValue(source, fields.Total.Value()).All(ctx, db)
```

`amounts` is `[]value.Nullable[decimal.Decimal]`. Output column names, IDs, enums, codecs and field operators come from the declared record. Grouping, HAVING and the selected window stay inside the CTE. For deterministic window selection, order the input explicitly. Ordering inside a CTE does not promise the final result's order; order the consuming query too.

Outer joins use the existing nullable scope adapters. A nullable aggregate remains singly nullable, while a non-null count becomes nullable on a missing join side. Selecting part of a model still requires a declared projection; CTEs do not create incompletely hydrated models.

## Validation and current limits

Names must be valid PostgreSQL identifiers. Foundry rejects invalid/zero definitions, incomplete projections, eager-loading options on the definition, conflicting names, dependency cycles and excessive AST/parameter bounds before execution. A CTE name cannot shadow any unqualified physical table used in the same statement, even inside another definition. Choose another CTE name or explicitly qualify that model's table declaration.

Definitions compile independently of surrounding row scopes. They may contain their own valid explicit correlations, but cannot capture a row from the statement consuming them. Parameters share one numbering sequence, with CTE bindings preceding the consuming statement's bindings. Failed compilation returns no executable statement or partial bindings. Runtime collection and streaming behavior follows the existing projection/model execution contracts.

This API declares nonrecursive, read-only definitions, including [set-operation inputs](model-set-operations.md). Use [typed recursive CTEs](model-recursive-ctes.md) for an anchor and an explicitly owned recursive step. Data-modifying CTE bodies are not available. Set operations alone do not enable recursive references.
