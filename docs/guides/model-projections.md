# Declared query projections

<a id="agent-entry"></a>
## Agent entry

- Declare: [Typed result rows](#recipe-declare-rows)
- Select: [Typed values and pages](#recipe-select-rows)

- [Grouped and scalar summaries](#recipe-grouped-rows)

## Overview

Further reading: [Execution](#execution-and-failures).

A projection returns a complete ordinary Go record with a handwritten
`database/orm/rows` schema. It has no table or write lifecycle. Its fields keep
concrete model IDs, enum types and `value.Nullable` wrappers.

<a id="recipe-declare-rows"></a>
## Declare the result

Declare the result struct, then bind its fields with `rows.Define`. The `Email` field maps to the application's `contact_email` column:

<!-- compiled-source: tests/fixtures/consumer/reports/projections.go#user-summary-record -->
```go
type UserSummary struct {
	ID model.ID[models.User]
	// Email is the buyer contact address selected for this report.
	Email    string `foundry:"column=contact_email"`
	Nickname value.Nullable[string]
	Status   models.Status
}
```

`models.Status` is the application's string-backed `//foundry:enum`, with `StatusActive = "active"` and `StatusDisabled = "disabled"`; generate its enum descriptor with the model package.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `countries`, `database`, `decimal`, `database/orm/expr`, `model`, `database/orm`, `database/orm/rows`, `value`.

`UserSummaryColumns` names each field and `UserSummaries` binds them:

<!-- compiled-source: tests/fixtures/consumer/reports/rows.go#user-summary -->
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

A [datatable](datatable.md) is different: it needs no `rows.Define` schema and no
`rows.Select` mappings. Its row DTO is marked `//foundry:dto role=row`, each
column declares the expression it selects, and `Spec.Source` returns the base
source. Use `rows.Define` for a SQL result that is not a table, such as a report
read with `rows.Read`; use a datatable's columns for rows that a table serves.

<a id="recipe-select-rows"></a>
## Select typed values

`rows.Select(rows.From(modelQuery), schema, mappings...)` selects a complete result.
Use `rows.Map(destination, expr.Value(column))` for any queryable column. Use
`rows.MapSelected(destination, selected)` for computations, aggregates and windows.
Mappings must cover every result field exactly once. Missing, duplicate, foreign
or incompatible mappings fail before SQL. `expr.Nullable` explicitly widens a
required selected value; it does not substitute a default for NULL.

Sources retain scopes, filters, ordering and limit/offset. A projection exposes
no model mutations or eager loading. For a reusable query source, wrap it in
`rows.RecordOf`, then `rows.AsRecord`; query output columns with `rows.At`.

`SummaryPage` reads the schema declared above as one page; `models.Users` is the application's generated table, and `database` and `orm` are framework packages:

<!-- compiled-source: tests/fixtures/consumer/reports/guide.go#paginate-projection -->
```go
func SummaryPage(ctx context.Context, db database.Executor, page orm.PageRequest) (orm.PageResult[UserSummary], error) {
	users := models.Users
	query := users.Query().Where(orm.Eq(users.Status, models.StatusActive)).
		OrderBy(orm.Asc(users.Email), orm.Asc(users.Key()))
	selected := rows.Select(rows.From(query), UserSummaries,
		rows.Map(UserSummaries.ID, expr.Value(users.Key())),
		rows.Map(UserSummaries.Email, expr.Value(users.Email)),
		rows.Map(UserSummaries.Nickname, expr.Value(users.Nickname)),
		rows.Map(UserSummaries.Status, expr.Value(users.Status)),
	)
	return rows.Read(selected).Paginate(ctx, db, page)
}
```

<a id="recipe-grouped-rows"></a>
## Grouped and scalar summaries

`BuyerTotals` holds `BuyerID model.ID[models.User]`, `Total` and `Average` as
`value.Nullable[decimal.Decimal]` and `Orders int64`; `BuyerTotalRows` binds those
fields with `rows.Define` as above, and `models.Orders` is the application's order
table. `BuyerTotalsByBuyer` groups orders by buyer:

<!-- compiled-source: tests/fixtures/consumer/reports/guide_summaries.go#model-projections -->
```go
func BuyerTotalsByBuyer() rows.Query[models.Order, BuyerTotals] {
	o := models.Orders
	return rows.GroupBy(rows.Select(rows.From(models.Orders.Query()), BuyerTotalRows.Row,
		rows.Map(BuyerTotalRows.BuyerID, expr.Value(o.BuyerID)),
		rows.MapSelected(BuyerTotalRows.Total, expr.AsSelected(expr.Sum(o.TotalCents))),
		rows.MapSelected(BuyerTotalRows.Orders, expr.AsSelected(expr.Count[models.Order]())),
		rows.MapSelected(BuyerTotalRows.Average, expr.AsSelected(expr.Avg(o.TotalCents))),
	), expr.Group(expr.Value(o.BuyerID)))
}
```

`rows.GroupBy` requires every selected
or ordered ordinary column to be grouped; it does not infer primary-key functional
dependencies. Counts return `int64`; integer/decimal sums and averages return
`value.Nullable[decimal.Decimal]`. An ungrouped aggregate over empty input yields
one record with zero counts and NULL summaries; grouped empty input yields no rows.

### Scalar terminals and subqueries

`rows.ReadScalar` keeps the selected value type through the terminal. `OrderedEmails`
reads one selected column, here `[]string`, in deterministic order; a joined or
derived source reads the same way:

<!-- compiled-source: tests/fixtures/consumer/reports/guide_summaries.go#read-scalar -->
```go
func OrderedEmails(ctx context.Context, db database.Executor) ([]string, error) {
	users := models.Users
	emails := rows.SelectScalar(rows.From(users.Query()), expr.AsSelected(expr.Row(expr.Value(users.Email))))
	return rows.ReadScalar(rows.OrderScalar(emails, expr.Asc(expr.Value(users.Email)))).All(ctx, db)
}
```

Scalar subqueries become selected values through `expr.AsSelected`. Nullable
subqueries keep their explicit nullable type. `ScalarSummaryQuery` takes two scalar
subqueries from the caller, `buyers` selecting `model.ID[models.User]` and `count`
selecting `int64`, and the ID of the user whose nickname is looked up. It returns the
query for `ScalarReport` rows, whose `BuyerID`, `Nickname` and `Orders` fields are
`value.Nullable` because an empty subquery yields NULL; `ScalarReports` is their
`rows.Define` schema:

<!-- compiled-source: tests/fixtures/consumer/reports/guide_summaries.go#scalar-projection -->
```go
func ScalarSummaryQuery(buyers rows.Scalar[models.Order, model.ID[models.User]], count rows.Scalar[models.Order, int64], nicknameOf model.ID[models.User]) rows.Query[models.User, ScalarReport] {
	u, o := models.Users, models.Orders
	last := rows.LimitScalar(rows.OrderScalar(buyers, expr.Desc(expr.Value(o.TotalCents))), 1)
	nicknames := rows.SelectScalar(rows.From(models.Users.Query().Where(orm.Eq(u.ID, nicknameOf))), expr.AsSelected(expr.Row(expr.Value(u.Nickname))))
	return rows.OrderBy(rows.Select(rows.From(models.Users.Query()), ScalarReports.Row,
		rows.Map(ScalarReports.ID, expr.Value(u.ID)),
		rows.MapSelected(ScalarReports.BuyerID, expr.AsSelected(rows.ScalarRowQuery(rows.From(models.Users.Query()), last))),
		rows.MapSelected(ScalarReports.Nickname, expr.AsSelected(rows.ScalarNullableRowQuery(rows.From(models.Users.Query()), nicknames))),
		rows.MapSelected(ScalarReports.Orders, expr.AsSelected(rows.ScalarRowQuery(rows.From(models.Users.Query()), count))),
	), expr.Asc(expr.Value(u.Email)))
}
```

For outer-row correlation, see [correlated subqueries](model-correlations.md).

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

### Boolean fields and framework enums

`countries` is the framework package `github.com/weiloon1234/Foundry-Go/countries`; this projection declares its own row and column mapping.

Use `rows.BoolOf(b, &r.Enabled)` for built-in or named Boolean fields. Use
`rows.EnumOf(b, &r.Status)` when Status is `countries.Status`: it already exports
`EnumDescriptor()` and `StatusCodec()`. The enum constructor retains enum validation
and aggregate restrictions; a custom string field is not equivalent metadata.
[Compiled excerpt, imports and enclosing context](../../tests/fixtures/goway/migration/pilot2_test.go):

<!-- compiled-source: tests/fixtures/goway/migration/pilot2_test.go#pilot2-boolean-enum -->
```go
type CountryFlag struct {
	Status  countries.Status
	Enabled bool
}
type CountryFlagColumns struct {
	rows.Row[CountryFlag]
	Status  rows.Field[CountryFlag, countries.Status]
	Enabled rows.Field[CountryFlag, bool]
}

var CountryFlags = rows.Define(func(b rows.Builder[CountryFlag], r *CountryFlag) CountryFlagColumns {
	return CountryFlagColumns{b.Row(), rows.EnumOf(b, &r.Status), rows.BoolOf(b, &r.Enabled)}
})
```
