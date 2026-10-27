# Declared query projections

<!-- compiled-examples -->

A projection returns a complete ordinary Go record with a handwritten
`database/orm/rows` schema. It has no table or write lifecycle. Its fields keep
concrete model IDs, enum types and `value.Nullable` wrappers.

## Declare the result

Declare the result struct, then bind its fields with `rows.Define`:

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/reports/rows.go):

<!-- go-source: tests/fixtures/consumer/reports/rows.go -->
```go
type UserSummaryColumns struct {
	rows.Row[UserSummary]
	ID rows.Field[UserSummary, model.ID[models.User]]
	// Email is the buyer contact address selected for this report.
	Email    rows.Field[UserSummary, string]
	Nickname rows.Field[UserSummary, value.Nullable[string]]
	Status   rows.Field[UserSummary, models.Status]
}

var UserSummaries = rows.Define(func(b rows.Builder[UserSummary], r *UserSummary) UserSummaryColumns {
	return UserSummaryColumns{Row: b.Row(),
		ID:       rows.IDOf(b, &r.ID),
		Email:    rows.TextOf(b, &r.Email),
		Nickname: rows.NullableTextOf(b, &r.Nickname),
		Status:   rows.EnumOf(b, &r.Status),
	}
})
```

`rows.Field[R,V]` names a destination with an exact result type. Constructors
retain codec/capability information. Declarations are validated before use;
`rows.Check` can validate a schema during startup. The builder binds actual field
pointers, so field names and offsets do not need a parallel string schema.

### Public report DTOs

SQL rows and public JSON have separate declarations. Use an ordinary
`//foundry:dto` on an explicitly public struct, alongside its handwritten rows
schema. The DTO generator still owns JSON/validation descriptors. Keep type and
package identity, JSON tags and descriptor names when migrating a report.
[Reporting](datatable.md) uses this arrangement for Members, Orders and Totals.

## Select typed values

`rows.Select(rows.From(modelQuery), schema, mappings...)` selects a complete result.
Use `rows.Map(destination, expr.Value(column))` for row values and
`rows.MapSelected(destination, selected)` for computations, aggregates and windows.
Mappings must cover every result field exactly once. Missing, duplicate, foreign
or incompatible mappings fail before SQL. `expr.Nullable` explicitly widens a
required selected value; it does not substitute a default for NULL.

Sources retain scopes, filters, ordering and limit/offset. A projection exposes
no model mutations or eager loading. For a reusable query source, wrap it in
`rows.RecordOf`, then `rows.AsRecord`; query output columns with `rows.At`.

## Grouped and scalar summaries

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go):

<!-- go-source: tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go -->
```go
o := models.Orders
totals := rowset.GroupBy(rowset.Select(rowset.From(models.Orders.Query()), reports.BuyerTotalRows.Row,
	rowset.Map(reports.BuyerTotalRows.BuyerID, expr.Value(o.BuyerID)),
	rowset.MapSelected(reports.BuyerTotalRows.Total, expr.Sum(o.TotalCents).Value()),
	rowset.MapSelected(reports.BuyerTotalRows.Orders, expr.Count[models.Order]().Value()),
	rowset.MapSelected(reports.BuyerTotalRows.Average, expr.Avg(o.TotalCents).Value()),
), expr.Group(expr.Value(o.BuyerID)))
```

The `rowset` import names `database/orm/rows`. `rows.GroupBy` requires every selected
or ordered ordinary column to be grouped; it does not infer primary-key functional
dependencies. Counts return `int64`; integer/decimal sums and averages return
`value.Nullable[decimal.Decimal]`. An ungrouped aggregate over empty input yields
one record with zero counts and NULL summaries; grouped empty input yields no rows.

## Filter and order groups

`rows.Having(q, conditions...)` accepts `expr.HavingPredicate[S]`; conditions and
repeated calls combine with AND. `expr.Grouped(rowPredicate)` lifts an explicitly
grouped row condition. Use `expr.HavingAnd`/`HavingOr` and predicate `Not` to compose.
NULL comparisons remain unknown, including after ordinary SQL negation. HAVING
can remove the single ungrouped aggregate result.

`rows.OrderBy` appends selected orders. `rows.ReorderUnwindowed` replaces ordering
for a report but rejects an existing outer limit/offset or DISTINCT ON. Use a
unique final key for pagination. An aggregate used only in ordering still causes
grouping. [Filtered aggregates](model-aggregate-filters.md) retain these rules.

## Execution and failures

| Input | Read adapter | Compile / context preflight / streaming |
| --- | --- | --- |
| `rows.Query[S,R]` | `rows.Read(q)` | `rows.CompileRead`, `rows.PreflightRead`, `rows.EachRead` |
| `rows.Scalar[S,V]` | `rows.ReadScalar(q)` | `rows.CompileRead`, `rows.PreflightRead`, `rows.EachRead` |
| Complete record selection | `rows.ReadRecord(q)` | Same `*Read` functions |
| Record / value set | `rows.ReadSet(q)` / `rows.ReadValues(q)` | Same `*Read` functions |

Read handles expose `All`, `First`, `RequireFirst`, `Result.Count` and `Exists`. `First`
returns `value.Optional[R]`; `RequireFirst` reports `database.NotFound`. Count
observes grouping, HAVING, DISTINCT and the selected window. `Limit(0)` stays empty.
Use `rows.Read(q).Count` for projection counts.

Collected results are discarded on decode failure. Streams close on callback
error, cancellation and decode failure; earlier callback effects remain the
caller's responsibility. Complete model reads may run retrieval callbacks in
bounded batches after closing rows. DTO/scalar streams decode one result at a time.
Transactions remain caller-owned; avoid another operation on an open transaction
cursor. Compile/preflight do no I/O and preserve validation errors.

`rows.Read(query).Paginate` and `rows.Read(query).SimplePaginate` require explicit ordering and reject an
existing outer LIMIT or nonzero OFFSET. The same rule applies to common result
read handles; nested input windows remain intact. Simple pages use one lookahead
row without a count.
A total and data read need a suitable transaction isolation level for one snapshot.
See [pagination](model-pagination.md), [derived records](model-subqueries.md),
[joins](model-joins.md) and [transaction-required composition](transaction-query-composition.md).

For declaration and API migration, see the [ORM migration guide](orm-migration.md).

### Boolean fields and framework enums

Use `rows.BoolOf(b, &r.Enabled)` for built-in or named Boolean fields. Use
`rows.EnumOf(b, &r.Status)` when Status is `countries.Status`: it already exports
`EnumDescriptor()` and `StatusCodec()`. The enum constructor retains enum validation
and aggregate restrictions; a custom string field is not equivalent metadata.
See the compiled [row declaration](orm-migration.md#pilot-expressions-and-row-declarations).
