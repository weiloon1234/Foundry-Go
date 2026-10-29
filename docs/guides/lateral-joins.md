# Typed lateral joins

<!-- compiled-examples -->

A lateral source returns complete records for each preceding outer row. Build an
explicit correlation, then select a complete inner record or a handwritten rows
projection. `rows.AsLateral[Tag]` retains the required outer owner; lateral join
helpers require that owner as their left input.

## Select records per parent

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/lateralqueries/records_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/lateralqueries/records_postgres_test.go -->
```go
func userOrders[L any](outer rows.ScopeSource[L], scope expr.Scope[L, models.User]) (rows.CorrelatedSource[L, rows.Alias[ordersAlias, models.Order]], expr.Scope[rows.Correlation[L, rows.Alias[ordersAlias, models.Order]], models.Order]) {
	orders := rows.As[ordersAlias](models.Orders.Query(), "orders_for_user")
	c := rows.Correlate(outer, orders)
	inner := rows.InnerScope(c, rows.Scope(orders))
	u, o := rows.OuterScope(c, scope), inner
	return c.Where(expr.Equal(expr.At(o, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(u, orm.KeyExpr(models.Users.ID)))), inner
}
```

The linked package uses `SelectCorrelatedRecord(c, scope)` with an order and
`Limit(1)`, then `AsLateral` and `LeftJoinLateral` to select one order per user.
`CrossJoinLateral`/`InnerJoinLateral` drop parents without an inner result;
`LeftJoinLateral` retains them with nullable right fields. Use `LateralScope` and
the usual preserved/nullable scope helpers to map outputs.

## Preserve parents without matches

Inner and left lateral joins accept typed ON conditions; no conditions means
ON TRUE. Inner WHERE runs before the inner limit. ON filters those candidates;
final WHERE can remove the preserved parent. An outer limit bounds the completed
result, not each parent's candidate set. Supply final ordering separately.

## Declared reports

`rows.ProjectCorrelated(c, schema, mappings...)` creates a correlated record from
ordinary model/rows fields and selected expressions. An aggregate without grouping
returns one row even for no matches: count zero, sum NULL. HAVING can remove it.
`rows.HavingCorrelatedRecord` and `DistinctCorrelatedRecord` keep the outer owner.
Window expressions retain their own SELECT phase.

A complete preserved lateral result can use `SelectRecord`; missing sides need
nullable projection fields. Read results compose with ordinary pagination, CTEs,
sets and explicit transaction-required locks.

## Ownership and validation

Correlated records cannot execute independently or become ordinary uncorrelated
aliases/CTEs. No right/full correlated lateral join is exposed: a preserved right
side cannot depend on an absent left row. Concrete alias visibility, grouping,
window bounds and complete result mappings are validated before I/O, including
zero-limit reads. Ordinary derived sources still reject accidental correlation.

Use [transaction correlations](transaction-correlations.md) when locks belong to
the dependent SELECT. Parameter binding and decoding share the normal
[projection runtime](model-projections.md).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
