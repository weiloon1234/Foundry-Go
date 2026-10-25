# Typed common table expressions

<!-- compiled-examples -->

`rows.CTE(name, recordSource)` defines a reusable typed SELECT. Use
`rows.ModelRecord(query)` for a complete model, or `rows.RecordOf(projection)` for
a handwritten rows projection. Alias the definition with `rows.AsRecord[Tag]`.

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/ctes/ctes_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/ctes/ctes_postgres_test.go -->
```go
u := models.Users
eligible := rowset.CTE("eligible_users", rowset.ModelRecord(models.Users.Query().Where(u.Age.Gte(21)))).Materialized()
first, second := rowset.AsRecord[eligibleAlias](eligible, "first_user"), rowset.AsRecord[otherAlias](eligible, "second_user")
a, b := rowset.Scope(first), rowset.Scope(second)
joined := rowset.InnerJoin(first, second, rowset.On(expr.At(a, models.Users.ID), expr.At(b, models.Users.ID)))
fields := rowset.LeftScope(joined, rowset.Scope(first))
names := rowset.OrderScalar(rowset.SelectScalar(joined, expr.Select(expr.At(fields, models.Users.Email))), expr.At(fields, models.Users.Email).Asc())
```

The `rowset` import names `database/orm/rows`; the linked package declares both
alias tags. `expr.At` reads model columns through the alias scope. `rows.At` reads
fields of a rows schema. Each alias preserves the complete result's type and codec.

## Definition ownership

A descriptor carries its definition. Reusing it emits one WITH entry, and dependent
CTEs are ordered before their consumers. Distinct definitions with one SQL name,
invalid identifiers, dependency cycles and physical-table shadowing fail compilation.
Use `Materialized` or `NotMaterialized` on the definition before sharing aliases.
PostgreSQL owns whether an allowed inlining request can be applied.

CTEs retain their inner filters, grouping, HAVING, distinctness and limit/offset.
Bindings share statement-wide numbering. CTEs are read-only sources; they do not
implicitly capture outer rows or carry eager-loading pipelines. Independent
SELECT scopes remain independent, but compilation shares statement-wide depth,
node and parameter limits across CTE bodies and the consuming SELECT.
Inner ordering does not promise final result order; order the consuming query
explicitly. Materialization controls SQL execution, not an application cache.

## Reading and composition

Use `rows.SelectRecord` for a preserved complete record, `rows.Select` for a new
projection or `rows.SelectScalar` for one selected value. Compile and stream through
the [read terminals](model-projections.md#execution-and-failures). A CTE does not
execute until an enclosing read executes.

For recursive definitions use [recursive CTEs](model-recursive-ctes.md). For locked
inputs use [transaction-required CTEs](transaction-query-composition.md); an ordinary
CTE cannot erase a transaction requirement.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
