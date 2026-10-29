# Typed set operations

<!-- compiled-examples -->

`database/orm/rows` combines complete record sources with `Union`, `UnionAll`,
`Intersect`, `IntersectAll`, `Except` and `ExceptAll`. Use `rows.ModelRecord` for a
model or `rows.RecordOf` for a projection. Inputs can have different query scopes,
but must return the same record type and compatible column/codec declarations.

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/setqueries/editor_examples_test.go):

<!-- go-source: tests/fixtures/consumer/setqueries/editor_examples_test.go -->
```go
func derivedReports() rows.SetQuery[reports.BuyerTotals] {
	u, o, r := models.Users, models.Orders, reports.BuyerTotalRows
	// Both reports return BuyerTotals while selecting from different model scopes.
	orders := rows.GroupBy(rows.Select(rows.From(o.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(orm.ColumnExpr(o.BuyerID))), rows.MapSelected(r.Total, expr.Sum(orm.NumberExpr(o.TotalCents)).Value()),
		rows.MapSelected(r.Orders, expr.Count[models.Order]().Value()), rows.MapSelected(r.Average, expr.Avg(orm.NumberExpr(o.TotalCents)).Value())), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID))))
	people := rows.GroupBy(rows.Select(rows.From(u.Query()), r.Row,
		rows.Map(r.BuyerID, expr.Value(orm.KeyExpr(u.ID))), rows.MapSelected(r.Total, expr.Sum(orm.NumberExpr(u.Age)).Value()),
		rows.MapSelected(r.Orders, expr.Count[models.User]().Value()), rows.MapSelected(r.Average, expr.Avg(orm.NumberExpr(u.Age)).Value())), expr.Group(expr.Value(orm.KeyExpr(u.ID))))
	return rows.UnionAll(rows.RecordOf(orders), rows.RecordOf(people))
}
```

## Records and single values

The combined record scope is `rows.Set[R]`. Read a field with
`rows.At(combined.Scope(), schema.Field)` and apply output filters/orders there.
Use `rows.ReadSet` for common read terminals. `rows.AsRecord` introduces an alias
when the set becomes an input to another SELECT.

For one column, wrap a scalar source in `rows.Values` and use `UnionValues`,
`UnionAllValues`, `IntersectValues`, `IntersectAllValues`, `ExceptValues` or
`ExceptAllValues`. `ValueSetQuery.Value()` exposes the combined typed operand;
`rows.ReadValues` supplies common terminals. Required and nullable types do not
silently mix; widen explicitly before combining.

## Multiplicity and boundaries

UNION removes duplicates; UNION ALL retains them. INTERSECT ALL retains the
minimum multiplicity, and EXCEPT ALL subtracts right multiplicity down to zero.
Comparison is SQL row comparison, including SQL duplicate treatment of NULL.

Each input keeps its own ordering and window inside its SELECT boundary. Ordering
and pagination applied to the combined result operate after the set operation.
Input order does not promise final order, and no primary-key order is added.
Numbered/simple result pages require explicit ordering and no existing outer
LIMIT or nonzero OFFSET. Mixed set operations preserve the declared tree with
parentheses, rather than relying on SQL operator precedence.
Count observes the final selected window, including duplicates and limit/offset.
Collected decoding failures discard all results; streaming closes on all exits.

`rows.DistinctSet` and `rows.DistinctValues` deduplicate output. Selected-key
`rows.DistinctOnValueSet` validates ordering prefixes like other DISTINCT ON
families. Derived aggregates such as `expr.SumNullableRow(rows.At(...))` require
the output field's numeric capability. No model writes or eager loading are
introduced by a set operation. Go record compatibility cannot prove the physical
SQL types, collations or equality support; those remain database checks.

See [derived records](model-subqueries.md), [distinct reads](model-distinct.md) and
[read terminals](model-projections.md#execution-and-failures).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
