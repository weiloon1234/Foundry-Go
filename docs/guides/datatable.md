# Typed datatables and exports

<a id="agent-entry"></a>
## Agent entry

- Declare: [Public rows and columns](#recipe-declare-table)
- Select: [Joined and grouped tables](#recipe-join-group-tables)
- Filter: [Typed adapters](#recipe-table-filter-adapters), [Outer-joined text](#recipe-filter-outer-joined-text)
- Authorize: [Current server authority](#recipe-authorize-table)
- Register: [Standard routes and tickets](#recipe-table-routes)
- Bind custom endpoints: [HTTP operation bindings](#recipe-table-operation-bindings)
- Export: [Bounded artifacts](#recipe-export-table)
- Consume: [Generated table bindings](client-contracts.md#recipe-table-bindings)
- Declare SQL rows: [Typed row schemas](model-projections.md#recipe-declare-rows)

## Overview

Datatables bind handwritten ORM sources to explicit DTO contracts. Members,
Orders and Totals in the [reporting consumer](../../tests/fixtures/consumer/reporting/)
cover model, joined and grouped sources.

<a id="recipe-declare-table"></a>
## Declare one row and its columns

Declare an explicit response DTO:

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/reporting/models.go):

<!-- compiled-source: tests/fixtures/consumer/reporting/models.go#datatable -->
```go
//foundry:dto
type MemberRow struct {
	ID       model.ID[Member]       `json:"id"`
	Name     string                 `json:"name"`
	Nickname value.Nullable[string] `json:"nickname"`
	State    State                  `json:"state"`
	Balance  decimal.Decimal        `json:"balance"`
}
```

The ordinary DTO annotation generates the JSON descriptor and validation field
selectors. `MemberRowJSON()` owns its wire graph; `MemberRowValidationFields()` owns JSON names and Go getters.
A handwritten `rows.Define` schema owns SQL output mapping. SQL projections do
not become public DTOs implicitly. Persistence models, persistence tags and
unsupported custom codecs remain rejected at DTO boundaries. Use JSON tags for public property names
and typed projection mappings for the selected SQL expressions.

`datatable.DefineColumn[Source](wire.Name, label, options...)` returns a typed
`ColumnRegistration[Source, Row]` directly. Supply functional options
`datatable.SortBy(expression)`, `datatable.FilterBy(source)`,
`datatable.Searchable(rowExpression)` and
`datatable.ExportAs(selectedExpression, cell)` only for supported capabilities.
`ColumnOption[Source, Value]` retains both the table scope and DTO field type;
mismatched scopes and values fail compilation. `Searchable` infers its types from
the row expression; `ExportAs` uses a selected expression, such as `expr.AsSelected(name)`
or `expr.AsSelected(count)`, so neither option repeats `[Source]`. These expressions supply
types only; exports format the DTO field value.
The table checks each column against a scalar property in the
row's existing JSON graph, including enum cases, widths, nullability and formats.
Arrays, objects and dynamic fields cannot be scalar table columns.

Labels use `i18n.MessageKey`, independent of the locale. Keys use the shared
semantic identifier grammar; constructing a key does not prove catalog
membership. The manager borrows a locale catalog and heading resolver for exports.
The [localization guide](localization.md) covers UI catalogs, enum label
declarations (`//foundry:enum labels=…`) and locale resolution. Clients consume
the [generated enum metadata](client-contracts.md#recipe-table-bindings); keep
unknown received values distinct from the declared choices.

The [row-schema recipe](model-projections.md#recipe-declare-rows) owns handwritten
SQL destinations. This table combines computed text, enum, nullable and exact
decimal columns, a relation filter, stable ordering and tenant-scoped reads:

<!-- compiled-source: tests/fixtures/consumer/reporting/members.go#member-table -->
```go
var Members = datatable.Define(memberSpec())

func memberSpec() datatable.Spec[Member, MemberRow, Authority] {
	fields, wire := MemberModels, MemberRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	state := foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor())
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	name := expr.Trim(expr.Value(orm.TextExpr(fields.Name)))
	id := expr.Row(expr.Value(orm.KeyExpr(fields.ID)))
	nickname := expr.TextNullableRow(orm.NullableTextExpr(fields.Nickname))
	stateExpr := expr.CompareRow(expr.Value(orm.EnumExpr(fields.State)))
	balance := expr.OrderRow(expr.Value(orm.NumberExpr(fields.Balance)))
	return datatable.Spec[Member, MemberRow, Authority]{
		ID: "reports.members", Row: MemberRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Member, MemberRow]{
			datatable.DefineColumn[Member](wire.ID, MemberIDLabel, datatable.SortBy(expr.AsSelected(id)), datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Member]()))),
			datatable.DefineColumn[Member](wire.Name, MemberNameLabel, datatable.SortBy(expr.AsSelected(name)), datatable.FilterBy(datatable.Where(text, expr.TextFilter(name))), datatable.Searchable(name), datatable.ExportAs(expr.AsSelected(name), datatable.ScalarCell(text))),
			datatable.DefineColumn[Member](wire.Nickname, NicknameLabel, datatable.SortBy(expr.AsSelected(nickname)), datatable.FilterBy(datatable.NullableWhere(text, expr.NullableTextFilter(nickname))), datatable.ExportAs(expr.AsSelected(nickname), datatable.NullableCell(text))),
			datatable.DefineColumn[Member](wire.State, StateLabel, datatable.FilterBy(datatable.Where(state, expr.CompareFilter(stateExpr))), datatable.ExportAs(expr.AsSelected(stateExpr), datatable.ScalarCell(state))),
			datatable.DefineColumn[Member](wire.Balance, BalanceLabel, datatable.SortBy(expr.AsSelected(balance)), datatable.FilterBy(datatable.Where(amount, expr.OrderFilter(balance))), datatable.ExportAs(expr.AsSelected(balance), datatable.ScalarCell(amount))),
		},
		Filters: []datatable.FilterRegistration[Member]{
			datatable.DefineFilter("orderLabel", OrderLabel, datatable.Related(datatable.Where(text, expr.TextFilter(expr.TextRow(orm.TextExpr(OrderModels.Label)))), func(predicate expr.Predicate[Order]) expr.Predicate[Member] {
				return expr.Has(MemberModelRelations.Orders, predicate)
			})),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.Name), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[Member]{expr.Asc(expr.Value(orm.KeyExpr(fields.ID)))},
		Authorize:   authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[Member, MemberRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[Member, MemberRow]{}, err
			}
			return rows.Select(rows.From(MemberModels.Query().Where(orm.NumberEq(fields.TenantID, actor.TenantID))), MemberRows,
				rows.Map(MemberRows.ID, expr.Value(orm.KeyExpr(fields.ID))), rows.MapSelected(MemberRows.Name, expr.AsSelected(name)),
				rows.Map(MemberRows.Nickname, expr.Value(orm.NullableTextExpr(fields.Nickname))), rows.Map(MemberRows.State, expr.Value(orm.EnumExpr(fields.State))), rows.Map(MemberRows.Balance, expr.Value(orm.NumberExpr(fields.Balance)))), nil
		},
	}
}
```

Regenerate public DTO descriptors after a DTO change. SQL row-schema changes
alone need no generation. Re-export client contracts after public table metadata
or route changes; see [export ownership](client-contracts.md#recipe-export-contracts).

<a id="recipe-table-filter-adapters"></a>
## Migrate report sources to ORM declarations

`Spec.Source` returns `rows.Query[S, R]`. Keep the DTO type, package, JSON
descriptor and validation fields;
replace SQL projection selection with a handwritten `rows.Define` schema and
`rows.Select` mappings. The [report declarations](../../tests/fixtures/consumer/reporting/orm.go)
use `orm.PrimaryOf` and `orm.SoftDeleteOf` to retain declaration roles; the
[row schemas](../../tests/fixtures/consumer/reporting/rows.go) retain exact DTO
field types. Existing generated DTO descriptors continue to own wire metadata.

Use `expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(Users.Name))))` in `datatable.SortBy`, `expr.AsSelected` on an enum or comparison row
adapter (such as `expr.AsSelected(expr.CompareRow(expr.Value(orm.EnumExpr(Users.State))))`),
or `expr.AsSelected` on a computed or aggregate expression. `Stable` is `[]expr.ProjectionOrder[S]`,
accepting the short `expr.Asc(expr.Value(orm.KeyExpr(table.Key())))` spelling for a unique final key.
Filters wrap row expressions in typed filter adapters:

| Operand | Filter adapter |
| --- | --- |
| Required text | `expr.TextFilter(expr.TextRow(orm.TextExpr(Users.Name)))` |
| Nullable text | `expr.NullableTextFilter(expr.TextNullableRow(orm.NullableTextExpr(Users.Nickname)))` |
| Required number | `expr.OrderFilter(expr.OrderRow(expr.Value(orm.NumberExpr(Users.Age))))` |
| Nullable number | `expr.NullableOrderFilter(expr.OrderNullableRow(expr.Value(orm.NullableNumberExpr(Users.Score))))` |
| Enum or equality-only field | `expr.CompareFilter(expr.Value(orm.EnumExpr(Users.State)))` |
| Nullable enum | `expr.NullableCompareFilter(expr.Value(orm.NullableEnumExpr(Users.OptionalState)))` |
| Computed boolean | `expr.CompareFilter(expr.JSONContains(settings, wanted))` |
| Computed scalar with equality only | `expr.CompareFilter(expr.Length(name))` |
| Nullable computed value | `expr.NullableCompareFilter(matches)` |
| Joined text | `expr.TextFilter(expr.TextRowAt(memberScope, orm.TextExpr(Members.Name)))` |
| Required ordered aggregate | `expr.OrderValueFilter(expr.Count[User]())` |
| Nullable ordered aggregate | `expr.NullableOrderValueFilter(expr.Sum(orm.NumberExpr(Orders.Amount)))` |
| Equality-only aggregate | `expr.ValueFilter(expr.Exists[User]())` |
| Selected text | `expr.TextValueFilter(expr.TextValue(selectedName))` |

A nullable ordered field retains its nullable value type while the codec parses
only present scalar values:

<!-- compiled-source: tests/fixtures/consumer/reports/datatable_filters.go#nullable-ordered-filter -->
```go
func MeasurementScoreFilter() datatable.FilterSource[models.Measurement, value.Nullable[float64]] {
	score := expr.OrderNullableRow(expr.Value(orm.NullableNumberExpr(models.Measurements.Score)))
	return datatable.NullableWhere(http.FloatQuery[float64](), expr.NullableOrderFilter(score))
}
```

`CompareFilter` accepts any `expr.RowValue[S, V]`, including columns and computed
values. Use it with `datatable.Where(codec, expr.CompareFilter(row))` for equality
and membership without ordering or text operators. `NullableCompareFilter` accepts
`expr.RowValue[S, value.Nullable[V]]`; pair it with `datatable.NullableWhere` and a
codec for present `V` values. It also supplies SQL null tests. Both adapters retain
the exact scope and value type. Selected values and aggregates cannot enter these
row filters. Existing `CompareRow` and `CompareNullableRow` inputs still work.

`Related` maps `expr.Predicate[Child]` to `expr.Predicate[Parent]`, normally
with `expr.Has(ParentRelations.Children, predicate)`. `expr.ModelPredicate`
adapts an existing `orm.Predicate`. Use `rows.GroupBy` and `rows.Having` for grouped
reports, and `rows.MapSelected` for aggregates. Text adapters validate the actual
column capability, including when the field is embedded or aliased. Enums keep
their equality-only filter operators; LIKE still requires `AllowLike()`.

Request and response fields, operator lists and DTO identities are retained.
WHERE/HAVING mixing still rejects OR and NOT; nullable negation still includes
SQL NULL. Pagination and export retain their limits and read-only repeatable-read
transactions. Exports call `rows.EachRead` with the bounded lookahead limit and never
collect all rows in memory.

<a id="recipe-join-group-tables"></a>
## Join and group table sources

Joined tables retain both typed scopes. Their stable key identifies a result row:

<!-- compiled-source: tests/fixtures/consumer/reporting/orders.go#joined-table -->
```go
func orderSpec() datatable.Spec[OrderScope, OrderRow, Authority] {
	members := rows.As[memberSide](MemberModels.Query(), "members")
	orders := rows.As[orderSide](OrderModels.Query(), "orders")
	m, o := rows.Scope(members), rows.Scope(orders)
	joined := rows.InnerJoin(members, orders, rows.On(expr.At(m, orm.KeyExpr(MemberModels.ID)), expr.At(o, orm.ColumnExpr(OrderModels.MemberID))))
	member := rows.LeftScope(joined, m)
	order := rows.RightScope(joined, o)
	wire := OrderRowValidationFields()
	text := foundryhttp.StringQuery[string]()
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberName := expr.TextRowAt(member, orm.TextExpr(MemberModels.Name))
	orderLabel := expr.TextRowAt(order, orm.TextExpr(OrderModels.Label))
	id := expr.Row(expr.At(order, orm.KeyExpr(OrderModels.ID)))
	orderAmount := expr.OrderRow(expr.At(order, orm.NumberExpr(OrderModels.Amount)))
	base := rows.Select(joined, OrderRows,
		rows.Map(OrderRows.ID, expr.At(order, orm.KeyExpr(OrderModels.ID))), rows.Map(OrderRows.MemberName, expr.At(member, orm.TextExpr(MemberModels.Name))),
		rows.Map(OrderRows.Label, expr.At(order, orm.TextExpr(OrderModels.Label))), rows.Map(OrderRows.Amount, expr.At(order, orm.NumberExpr(OrderModels.Amount))))
	return datatable.Spec[OrderScope, OrderRow, Authority]{
		ID: "reports.orders", Row: OrderRowJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[OrderScope, OrderRow]{
			datatable.DefineColumn[OrderScope](wire.ID, OrderIDLabel, datatable.ExportAs(expr.AsSelected(id), datatable.ScalarCell(foundryhttp.ModelIDQuery[Order]()))),
			datatable.DefineColumn[OrderScope](wire.MemberName, MemberNameLabel, datatable.SortBy(expr.AsSelected(expr.Row(memberName))), datatable.FilterBy(datatable.Where(text, expr.TextFilter(memberName))), datatable.Searchable(memberName), datatable.ExportAs(expr.AsSelected(memberName), datatable.ScalarCell(text))),
			datatable.DefineColumn[OrderScope](wire.Label, OrderLabel, datatable.FilterBy(datatable.Where(text, expr.TextFilter(orderLabel))), datatable.Searchable(orderLabel), datatable.ExportAs(expr.AsSelected(orderLabel), datatable.ScalarCell(text))),
			datatable.DefineColumn[OrderScope](wire.Amount, AmountLabel, datatable.SortBy(expr.AsSelected(orderAmount)), datatable.FilterBy(datatable.Where(amount, expr.OrderFilter(orderAmount))), datatable.ExportAs(expr.AsSelected(orderAmount), datatable.ScalarCell(amount))),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.MemberName), Direction: datatable.Ascending}},
		Stable:      []expr.ProjectionOrder[OrderScope]{expr.Asc(expr.At(order, orm.KeyExpr(OrderModels.ID)))}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[OrderScope, OrderRow], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[OrderScope, OrderRow]{}, err
			}
			return base.Where(expr.Eq(expr.At(member, orm.NumberExpr(MemberModels.TenantID)), actor.TenantID), expr.Eq(expr.At(order, orm.NumberExpr(OrderModels.TenantID)), actor.TenantID)), nil
		},
	}
}
```

Grouped tables map aggregates with `rows.MapSelected`, filter them with `Having`
or `NullableHaving`, and use a group key for stable ordering:

<!-- compiled-source: tests/fixtures/consumer/reporting/orders.go#grouped-table -->
```go
func totalSpec() datatable.Spec[Order, MemberTotal, Authority] {
	fields, wire := OrderModels, MemberTotalValidationFields()
	count, total := expr.Count[Order](), expr.Sum(orm.NumberExpr(fields.Amount))
	amount := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	memberID := foundryhttp.ModelIDQuery[Member]()
	member := expr.CompareRow(expr.Value(orm.ColumnExpr(fields.MemberID)))
	return datatable.Spec[Order, MemberTotal, Authority]{
		ID: "reports.totals", Row: MemberTotalJSON(), Exports: true,
		Columns: []datatable.ColumnRegistration[Order, MemberTotal]{
			datatable.DefineColumn[Order](wire.MemberID, MemberIDLabel, datatable.FilterBy(datatable.Where(memberID, expr.CompareFilter(member))), datatable.ExportAs(expr.AsSelected(member), datatable.ScalarCell(memberID))),
			datatable.DefineColumn[Order](wire.Count, CountLabel, datatable.SortBy(expr.AsSelected(count)), datatable.FilterBy(datatable.Having(foundryhttp.IntegerQuery[int64](), expr.OrderValueFilter(count))), datatable.ExportAs(expr.AsSelected(count), datatable.ScalarCell(foundryhttp.IntegerQuery[int64]()))),
			datatable.DefineColumn[Order](wire.Total, TotalLabel, datatable.SortBy(expr.AsSelected(total)), datatable.FilterBy(datatable.NullableHaving(amount, expr.NullableOrderValueFilter(total))), datatable.ExportAs(expr.AsSelected(total), datatable.NullableCell(amount))),
		},
		DefaultSort: []datatable.Sort{{Column: validation.FieldName(wire.Total), Direction: datatable.Descending}},
		Stable:      []expr.ProjectionOrder[Order]{expr.Asc(expr.Value(orm.ColumnExpr(fields.MemberID)))}, Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.Query[Order, MemberTotal], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return rows.Query[Order, MemberTotal]{}, err
			}
			source := OrderModels.Query().Where(orm.NumberEq(fields.TenantID, actor.TenantID), orm.Has(OrderModelRelations.Member, orm.NumberEq(MemberModels.TenantID, actor.TenantID)))
			return rows.GroupBy(rows.Select(rows.From(source), MemberTotals,
				rows.Map(MemberTotals.MemberID, expr.Value(orm.ColumnExpr(fields.MemberID))), rows.MapSelected(MemberTotals.Count, expr.AsSelected(count)), rows.MapSelected(MemberTotals.Total, expr.AsSelected(total))), expr.Group(expr.Value(orm.ColumnExpr(fields.MemberID)))), nil
		},
	}
}
```

<a id="recipe-filter-outer-joined-text"></a>
## Filter outer-joined text

Use `AtNullable` for a required column made nullable by an outer join, and
`AtNullableValue` for an already-nullable column. `TextNullableRowValue` preserves
text operations and NULL semantics in both cases:

<!-- compiled-source: tests/fixtures/consumer/reports/datatable_filters.go#outer-joined-text -->
```go
func ReferralTextFilters() (datatable.FilterSource[ReferralScope, value.Nullable[string]], datatable.FilterSource[ReferralScope, value.Nullable[string]]) {
	referrals := rows.As[referralAlias](models.Users.Query(), "referral")
	sponsors := rows.As[sponsorAlias](models.Users.Query(), "sponsor")
	joined := rows.LeftJoin(referrals, sponsors, rows.On(
		expr.At(rows.Scope(referrals), orm.NullableColumnExpr(models.Users.IntroducerID)),
		expr.At(rows.Scope(sponsors), orm.KeyExpr(models.Users.ID)),
	))
	sponsor := rows.NullableRightScope(joined, rows.Scope(sponsors))
	text := http.StringQuery[string]()
	email := expr.TextNullableRowValue(expr.AtNullable(sponsor, orm.TextExpr(models.Users.Email)))
	nickname := expr.TextNullableRowValue(expr.AtNullableValue(sponsor, orm.NullableTextExpr(models.Users.Nickname)))
	return datatable.NullableWhere(text, expr.NullableTextFilter(email)),
		datatable.NullableWhere(text, expr.NullableTextFilter(nickname))
}
```

<a id="recipe-authorize-table"></a>
## Authorization and server scope

`datatable.Spec[S, R, A]` fixes the SQL scope, response type and trusted server
authority. `Authorize(ctx, authority, action)` and `Source(ctx, authority)` are
mandatory. Actions distinguish query, export and inspection. Public/system
reports still declare an explicit authorization callback.

The source supplies mandatory tenant, visibility and soft-delete restrictions.
Client predicates are added outside that scope. List, count and export all use
the same preparation and source pipeline. A request cannot replace the scope or
select arbitrary SQL names. Invalid names, operators and scalar values fail
before execution. Authorization runs before source construction, locale/heading
callbacks, temporary files and database work.

An interactive service can pass an `auth.Guard[M]` and invoke a registered typed
policy in its authorization callback. The consumer's private
[Authority](../../tests/fixtures/consumer/reporting/authority.go) supports both
authenticated requests and freshly resolved job actors. It shares the domain
permission decision and never accepts roles or authority in request JSON.

Retain the current request guard in authority, including after ticket redemption.
Do not snapshot the redeemed actor for deferred row loading:

<!-- compiled-source: tests/fixtures/consumer/reporting/authority.go#current-table-authority -->
```go
func FromGuard(guard auth.Guard[Operator]) Authority {
	return Authority{guard: value.Set(guard)}
}
```

Declare route access requirements as data and pass them to `ReadAccess` and
`ExportAccess`. Do not invoke authorization with a synthetic actor to discover
capabilities. An omitted access option adds no permission requirement; table
authorization still runs. The app owns any wrapper that shares these declarations
with its domain policy.

For [alternative route guards](authentication.md#recipe-any-of-guards), keep
separate typed read and export capability lists in the application's table
wrapper. Derive one `http.AllOf(http.Scopes(scopes), http.Permissions(permission))`
from each existing capability declaration, then pass the resulting
`[]http.GuardAlternative[Actor]` to `datatable.ReadAccess(http.AnyOf(read...))` and
`datatable.ExportAccess(http.AnyOf(export...))`. A single-capability table uses one
branch. Snapshot the capability lists and use the same declarations in the table
policy; do not copy names into a second route permission map.

An editorial read list can contain write and approve capabilities. Export keeps
its independently approved capability list; it never inherits read access.
Reject an empty list when the application requires an access declaration. Keep
export-disabled as an explicit table setting, not an empty `AnyOf`.

Export alternatives run at both ticket issuance and ticket-authenticated download.
Redemption rechecks current permissions and credential scope ceilings. The issuing
branch is not saved as an authorization grant. `Spec.Authorize`, tenant constraints
and scoped source construction continue to run after route admission. The
framework does not infer equality between arbitrary application policy callbacks.

Register each declaration once in the manager used by the routes:

<!-- compiled-source: tests/fixtures/consumer/reporting/http.go#register-tables -->
```go
func New(dependencies datatable.Dependencies, config datatable.Config) (*datatable.Manager, error) {
	return datatable.New(dependencies, config, Members.Registration(), NumericLevels.Registration(), Orders.Registration(), Totals.Registration())
}
```

Managers require the exact registered table declaration. A second declaration
with the same ID cannot substitute a different authorization callback. Registry
construction rejects duplicate IDs. `Table.Description()` is trusted assembly
metadata; use `Inspect(ctx, manager, authority)` when exposing it to clients.

## Requests, filtering and deterministic pages

Decode with `datatable.DecodeRequest(ctx, body)` or the generated
`datatable.RequestJSON()` transport contract. The bounded shared decoder rejects
unknown/duplicate JSON keys, invalid Unicode and wrong scalar representations.
For example:

```json
{
  "page": 1,
  "size": 20,
  "sort": [{"column": "name", "direction": "asc"}],
  "filters": [{"op": "between", "column": "balance", "values": ["1.25", "50"]}],
  "search": "Ada"
}
```

Rejected table request names, operators, values and bounds return an error matching
`http.BadRequest` and `fault.Invalid`. Return that error unchanged from a handler:
HTTP sends the existing localized `bad_request` response (400) without private
error text. The same classification applies to malformed `DecodeRequest` input
and invalid export format/presentation options. Decoding retains the underlying
`contract.DecodeError` and its owned issues for Go callers. A generated endpoint's
body decoder continues to provide its normal field paths in the public response;
table-specific rejections currently use the generic message.

Declaration/configuration faults, server source errors and output resource limits
remain internal failures; authorization, cancellation and capacity keep their
existing classifications. Never map every `fault.Invalid` to 400. No new DTO,
manifest version, database migration or catalog message is required. See the
[authenticated query consumer](../../tests/fixtures/consumer/reporting/http.go).

Zero/omitted page and size default to 1 and 20. Other invalid values are rejected,
not clamped. Sort names and directions use the declaration allowlist; repeated
sort columns fail. Client sorts replace the default sort, then the table appends
its mandatory `Stable` orders. The author must supply a unique final key for the
actual result, including joined or grouped rows.

Set `application.Settings.Features.Reports.Config.TextCollation` once to choose
the order for every text column sort in that application's manager. For example,
`expr.CollationUnicode` uses language-neutral Unicode ordering, or
`expr.ICUCollation("en-US")` chooses English ordering. The config-file spelling is
`features.reports.config.text_collation`:

```toml
[features.reports.config]
text_collation = "und-x-icu"
```

The default is empty, preserving database ordering. The manager applies this
setting to requested column sorts, `DefaultSort`, and exports using each sort
expression's database type metadata. Numeric expressions remain numeric even
when the DTO encodes them as JSON strings. An explicit
`SortBy(expr.AsSelected(expr.Collate(name, expr.CollationC)))` takes precedence;
the manager does not wrap that value with its default. `Spec.Stable` orders remain
unchanged. There is no per-table override; use an explicit collation on the sort
expression when needed. See [per-expression collation](scalar-calculations.md#per-expression-collation)
for supported names, validation and database availability.

The source must be unpaginated. `ReorderUnwindowed` replaces outer ordering and
rejects outer LIMIT/OFFSET and DISTINCT ON because they change result selection.
Materialize a DISTINCT ON result before reporting on its completed rows. Inner
windows and winner ordering remain intact. Ordinary DISTINCT stays intact.

Use `Where(codec, adapter)` or `Having(codec, adapter)` with the corresponding
row or selected filter adapter; nullable counterparts
retain the exact nullable column type while parsing non-null values with the
same codec. Available comparisons come from that typed expression. `Restrict`
narrows the operator allowlist. `Related` maps an explicitly declared child row
filter through a typed relation predicate. Extra filters need not be displayed
columns, but must have distinct declared names and labels.

The SQL pattern operator `like` is discovered for text sources but disabled
until the declaration calls `AllowLike()`, because a client-chosen pattern can
defeat indexes. Call it before `Restrict` when the restricted set includes
`like`; on a `Related` filter, enable it on the child source.

Values are scalar text, including integers and decimals. Empty string remains a
string; SQL NULL uses `is_null`/`is_not_null`, without values. Boolean trees use
`and`, `or` and single-child `not`. AND may contain WHERE and HAVING filters;
mixed-phase OR/NOT fail because moving them between phases would change meaning.

Negation means "the condition is not true". `ne` and `not_in` on a nullable
column also match NULL rows, and `not` pushes its negation down to each
comparison, adding `IS NULL` for a nullable operand, so `not(note eq "memo")`
includes rows without a note. SQL `NOT` alone would silently drop them.
NULL tests negate exactly, and a double negation restores the original filter.

Global search ORs only declared searchable columns. It prefers `icontains` when
the source supports it, otherwise literal `contains`. PostgreSQL owns case
folding. `%`, `_` and `!` stay literal for these substring operators; `like`
permits SQL pattern syntax only when explicitly allowed. `Define` rejects a
table whose searchable columns span the WHERE and HAVING phases.

`table.Query(ctx, manager, authority, request)` returns `orm.PageResult[R]` using one
count and one row query in a read-only repeatable-read transaction. The count
query omits the table's ordering. `table.SimpleQuery(...)` returns
`orm.SimplePage[R]` from a single row query with one lookahead row and no
count; use it when a total is not needed. `table.Count(...)` applies the same
filters and authorization with one unordered count query. Pagination is
validated for all three; count represents the complete matching result. The
default `MaxOffset` of 10,000 rows (page 500 at 20 rows) bounds deep offset
scans; use filters or an export beyond it. Failed queries return no partial
page. Direct Go callers retain ownership of request slices until the operation
returns.

<a id="recipe-export-table"></a>
## Complete, bounded export artifacts

Enable exports in the spec and mark exportable columns with `ScalarCell(codec)`
or `NullableCell(codec)`. These reuse the scalar codec for exact value formatting;
NULL renders as an empty cell. `FormatWith` supplies explicit human presentation
without changing the JSON/filter contract. The callback receives the validated
locale and time zone. Locale defaults to the catalog default; time zone defaults
to `Config.TimeZone`, inherited from the [application timezone](application-timezone.md)
in configured assembly or UTC for a direct manager. Explicit per-export
`Presentation.TimeZone` wins. Formatters must remain bounded and honor cancellation.

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/reporting/reporting_postgres_test.go):

<!-- compiled-source: tests/fixtures/consumer/reporting/reporting_postgres_test.go#datatable-2 -->
```go
artifact, err := reporting.Totals.Export(scope.Context(), f.manager, authority, datatable.Request{Filters: []datatable.Filter{{Op: datatable.Greater, Column: "total", Values: []string{"10"}}}}, datatable.ExportOptions{Format: datatable.CSV})
if err != nil {
	t.Fatal(err)
}
defer artifact.Close()
```

The export excerpt uses the test's authenticated scope and manager; the test owns
and closes the completed artifact. The manager streams query rows into that file.

Export validates page settings but exports **all matching rows**, bounded by
`MaxExportRows`. It streams one query into a private temporary file and returns
an artifact only after row decoding, formatting, CSV/ZIP finalization and seek
succeed. A lookahead row rejects oversized reports instead of silently truncating.
The filename is normalized as a display name and receives the selected extension.
It never controls a filesystem path or storage key.

One awkward value never fails an export. Invalid UTF-8 becomes U+FFFD. A cell
longer than `MaxCellBytes`, or an XLSX cell longer than the 32,767 UTF-16 units
a spreadsheet cell holds, is cut at a character boundary and ends with
`…[truncated]`. CSV carries control characters literally; XLSX stores C0
controls and U+FFFE/U+FFFF with the OOXML `_xHHHH_` escape. The formatted
cells of one row are bounded by `MaxRowBytes` as a resource limit.

CSV uses standard delimiter/newline/quote escaping and prefixes potentially
interpreted formula text with an apostrophe: `=`, `+`, `-`, `@`, including after
leading Unicode whitespace, and leading tab/CR. This changes the literal CSV
value, including negative numeric text. Re-saving/re-importing CSV can change a
spreadsheet's interpretation; consumers must retain a safe import policy.
`ExportOptions.ByteOrderMark` prefixes CSV with a UTF-8 byte-order mark for
spreadsheet programs that otherwise guess a legacy encoding; XLSX rejects it.

XLSX uses one worksheet and a fixed style sheet. `ScalarCell`/`NullableCell`
columns become typed cells when exactly representable: integers and decimals
with at most 15 significant digits (integers use format `0`), finite floats,
booleans, dates (`yyyy-mm-dd`) and date-times (`yyyy-mm-dd hh:mm:ss`) from
1900-03-01 to 9999-12-31. Date-times are written as wall time in the export's
presentation time zone, since spreadsheets store no zone. Longer integers (such
as 16-digit identifiers) and decimals, other dates, enumerations and every `FormatWith` column stay
exact inline text. Formulas, macros, hyperlinks, external links and a growing
shared-string table are absent. XML escaping and OOXML literal-escape handling
preserve text and carriage returns. The ZIP has a fixed number of parts and
streams buffered worksheet rows. Both compressed file bytes and uncompressed XML
bytes are bounded; there is no workbook-sized memory buffer. These are resource
bounds, not a throughput guarantee.

Capacity has two levels. `MaxExports` bounds concurrent generation (query,
formatting and file writing); its slot is released as soon as the artifact is
complete, so a slow download or delivery never blocks other exports.
`MaxArtifacts` (at least `MaxExports`) bounds completed artifacts still open,
and so the temporary files on disk; each holds its slot until `Close`. When
either is exhausted, an export waits briefly and then fails with
`fault.Overloaded` (HTTP 503 with `Retry-After`). Close is serialized and
idempotent. Cancellation stops generation but does not abandon an executing
callback or open file; a completed artifact's reads end only at `Close` or
manager shutdown. Failed file removals remain manager-owned, prevent new exports,
and are retried by Close, the next export and manager shutdown. A failed export
never publishes a partial artifact. Retain the manager if shutdown reports a
cleanup failure so removal can be retried after the underlying filesystem issue
is repaired. Applications should use a private, application-owned temporary
directory and normal operational cleanup for process-crash leftovers.

Defaults include 32 query operations, 2 concurrent export generations, 16
retained artifacts, one-minute query and ten-minute export timeouts, page size
at most 1000, offset at most 10,000, 50,000 export rows, 64 MiB file bytes and
256 MiB XML. Individual encoded rows are limited to 256 KiB and their page sum
to 4 MiB; exported rows bound their formatted cells by the same row limit.
Cells are limited to 64 KiB (at least 64 bytes) and XLSX cells to 32,767 UTF-16
units, with truncation as described above. Requests have a 64 KiB encoded
limit, 128 filter nodes, bounded nesting, 100 values per filter, 4096 bytes per
scalar/search and 8 requested sort fields. `DefaultConfig` is the runtime source
of these defaults; `Validate` checks custom limits before any I/O.

<a id="recipe-table-operation-bindings"></a>
## Declare HTTP operation bindings

Use `contract.TableID` for table identities. There is no `datatable.TableID` alias.
Declare each association at its endpoint. A query endpoint (`h` is the
Foundry-Go `http` package):

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/table_bindings.go#datatable-3 -->
```go
func tableQuery[S, R, A any](table datatable.Table[S, R, A], id h.RouteID, path string) h.RouteRegistration {
	endpoint := datatable.QueryEndpoint(table, h.DefineRoute(h.RouteSpec{ID: id, Method: h.POST, Access: h.Public}, h.StaticPath(path)), h.EmptyQuery())
	return endpoint.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, datatable.Request]) (pagination.NumberedResponse[R], error) {
		return pagination.NumberedResponse[R]{Data: []R{}}, nil
	})
}
```

Ticket export, download and direct file export bindings:

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/table_bindings.go#datatable-4 -->
```go
ticket := datatable.BindTicketExport(reporting.Members, h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "issue.report", Method: h.POST, Access: h.Public}, h.StaticPath("/issue")), h.EmptyQuery(), h.JSONBody(TableExportRequestJSON()), h.JSONResponse(201, TableExportResourceJSON())), TableExportRequestValidationFields().Request)
download := datatable.DownloadEndpoint(reporting.Members, h.DefineRoute(h.RouteSpec{ID: "files.open", Method: h.GET, Access: h.Public}, h.StaticPath("/file")), TableDownloadQueryDescriptor(), h.DownloadResponse("text/csv"))
aggregate := datatable.DownloadEndpoint(reporting.Totals, h.DefineRoute(h.RouteSpec{ID: "files.aggregate", Method: h.GET, Access: h.Public}, h.StaticPath("/aggregate")), h.EmptyQuery(), h.DownloadResponse("text/csv"))
direct := datatable.BindFileExport(reporting.Orders, h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "reports.direct", Method: h.GET, Access: h.Public}, TableFilePathDescriptor()), TableDownloadQueryDescriptor(), h.EmptyBody(), h.DownloadResponse("text/csv")))
```

A query route can use any ID, such as `admin.inventory` or a scoped
`admin-tables.members.query`; its name does not identify the table. `QueryEndpoint` supplies `RequestJSON()` and the canonical
`pagination.NumberedResponse[R]` contract from the table's retained row descriptor.
The handler and HTTP guard retain their concrete types. The guard actor and table
authority need not be the same type.

Ticket exports keep the complete application body and response, including status.
The selected validation field must belong to that body and have type `Request`.
It must name a required, non-null, top-level JSON property. A ticket response can
use any declared JSON DTO. Direct file exports keep the complete endpoint request;
they do not require a bare table request body. Downloads require GET, no body,
and a file response. Export and download bindings require table export capability.

Register the table with the manager or registry used by manifest export. A table
has one query, one export, and one download slot across all routes; ticket and
file exports compete for the export slot. Missing slots are legal. The low-level
`http.TableOperation(binding)` endpoint option uses the same manifest validation.
Repeated options fail even when their values agree. Bindings describe association;
application handlers still own authorization and the work they execute.

See [generated table helpers](client-contracts.md#typed-datatable-descriptors) for
mapped generic access and operation-based client projection.

## HTTP, jobs and lifecycle

`table.Download(...)` freezes a validated request and returns the existing typed
HTTP download. Opening it runs export authorization and scoping again. The
HTTP file response owns the artifact, ranges and cleanup. Its own file-response
limits still apply. Generation and transfer run under the request's single
deadline, so declare the download route with
`http.Timeout(manager.DownloadTimeout())` instead of inheriting the kernel's
`RequestTimeout`. `Manager.DownloadTimeout()` is twice `ExportTimeout` (20 minutes
by default, at most `http.MaxRouteTimeout`); generation is additionally bounded
by `ExportTimeout`. When the request ends through its deadline, a client
disconnect or shutdown, generation stops and promptly releases its export slot,
read transaction and temporary file. Deliver very large reports through a job.
Exports read committed rows only, in their own read-only snapshot. Each
response carries the artifact's SHA-256 as a strong `ETag` plus `Last-Modified`.
Every open regenerates the report, so a Range resume with `If-Range` receives
the complete new file when a later run differs instead of splicing two runs,
and `If-None-Match` returns 304 for identical content. See the
[consumer endpoint](../../tests/fixtures/consumer/reporting/http.go) and
[download guide](http-downloads.md).

`datatable.ExportHandler` adapts a table to an ordinary `jobs.Handler[P]`.
The resolver must reload current authority from a trusted queued reference; the
table then applies export authorization again. Delivery receives a completed
artifact, after its generation slot is released, and must finish reading before
returning. Its context retains the job's identity and the export deadline and
ends at manager shutdown; closing the same manager from delivery fails with a
cycle error. Cancellation cannot report success even if the delivery callback
returns nil. The handler closes the artifact on success, failure, panic and
Goexit. Jobs remain at-least-once; use the definition's
`CurrentID(ctx)` as an idempotent destination identity. The
[consumer declaration](../../tests/fixtures/consumer/reporting/export_job.go)
stores typed actor/request/presentation data, with no captured role claims.
Use the existing [jobs/outbox](jobs.md) path for durable transactional dispatch.

`datatable.Module` borrows database/locale dependencies and preserves their
lifetime until query and export-generation callbacks actually exit. Export
workers must depend on this module and shut down first. `Manager.Close(ctx)`
stops admission, cancels both operation groups and closes every completed
artifact that is still open: its reads report `fault.Closed` and its file is
removed. Completed artifacts use no borrowed dependency, so shutdown never
waits for their owners; the context bounds the caller's wait for callbacks.
Separate `DoneQueries`/`DoneExports` signals report actual exit. Self-shutdown
from an active table callback or export delivery fails before either group is
partially canceled.

`Registry.Descriptions()` contributes row/request schemas, labels, capability
allowlists and typed scalar metadata to the shared manifest. The [TypeScript emitter](client-contracts.md) consumes that shared manifest;
this package does not infer a second client schema.

## Importing CSV and XLSX

The `datatable/importer` package streams an uploaded table into a typed row
struct. Declare each heading once with its scalar codec and a typed assignment,
plus ordinary validation rules for the complete row. The
[consumer member import](../../tests/fixtures/consumer/reporting/member_import.go)
declares:

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/reporting/member_import.go):

<!-- compiled-source: tests/fixtures/consumer/reporting/member_import.go#datatable-5 -->
```go
func memberImports() importer.Definition[MemberImport] {
	text := foundryhttp.StringQuery[string]()
	name := validation.DefineField("name", func(row MemberImport) string { return row.Name })
	return importer.Define(importer.Spec[MemberImport]{
		Columns: []importer.Column[MemberImport]{
			importer.Field("Name", text, func(row *MemberImport, v string) { row.Name = v }),
			importer.NullableField("Nickname", text, func(row *MemberImport, v value.Nullable[string]) { row.Nickname = v }),
			importer.Field("State", foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor()), func(row *MemberImport, v State) { row.State = v }),
			importer.Field("Balance", foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), func(row *MemberImport, v decimal.Decimal) { row.Balance = v }),
		},
		Rules: []validation.Rule[MemberImport]{validation.Rules(name, validation.NonBlank[string](), validation.MaxLength[string](80))},
	})
}
```

The test below retains its small input to inspect decoded values. In an application,
process each bounded chunk in the handler without collecting the whole import:

[Compiled excerpt, imports and enclosing context](../../tests/fixtures/consumer/reporting/member_import_test.go):

<!-- compiled-source: tests/fixtures/consumer/reporting/member_import_test.go#datatable-6 -->
```go
var imported []importer.Row[reporting.MemberImport]
report, err := reporting.MemberImports.Run(t.Context(), importer.CSVSource(strings.NewReader(upload)), func(_ context.Context, rows []importer.Row[reporting.MemberImport]) error {
	imported = append(imported, rows...)
	return nil
})
```

The heading row (the first row unless `HeadingRow` says otherwise) maps headings
after trimming spaces and ignoring case; a leading byte-order mark is ignored and
extra file columns are ignored. A missing `Field` heading or a repeated declared
heading returns a `*importer.HeadingError` (a `fault.Invalid`) with one issue per
heading before any row is read. A `NullableField` heading may be absent, and its
empty cells assign `value.Null`; an empty `Field` cell is a `required` issue.

Each non-blank row is parsed first; a cell its codec rejects is a `type` issue
at `/<heading>`, and an oversized cell a `length` issue. Rules run only on a
completely parsed row and report their own field paths. Invalid rows are skipped
and reported in `Report.Failures` with their row number: the worksheet row for
XLSX, or the line on which the record starts for CSV. `Report.Failed` counts all
of them; at most `MaxFailures` are retained. Valid rows reach the handler in
chunks of `ChunkSize`, and a handler error stops the import. Handled chunks are
not undone by a later failure: run the handler inside a caller-owned transaction
and check `Report.Failed` for an all-or-nothing import. Codec, rule and handler
callbacks run synchronously; panic and Goexit become faults. Importer-owned
input failures retain their safe diagnostics and `fault.Invalid` identity;
use `errors.As` to inspect the underlying `*fault.Error` or `*importer.HeadingError`.
Errors returned by application readers, rules and handlers retain their causes
for `errors.Is`/`errors.As`, but their diagnostic text is redacted.

`importer.CSVSource(reader)` accepts RFC 4180 quoting; `WithComma` selects
another delimiter. `importer.XLSXSource(readerAt, size)` reads the first
worksheet, or `WithSheet(name)`, streaming its XML: shared strings (including
rich-text runs, without phonetic guides), inline strings, formula results,
booleans and numbers. A floating-point column receives a number exactly as
stored. Every other column, including text, accepts only numbers a spreadsheet
shows without rounding (at most 15 significant digits); a stored number that
15 digits cannot represent, such as a 16-digit identifier or
`0.30000000000000004`, is a `type` issue instead of a silently rounded value.
Export such identifiers as text; datatable exports already do. In date,
date-time and local date-time columns a number is a serial date; date-times are
read as wall time in the spec's `TimeZone` (UTC by default). Error cells and
references to missing shared strings are `type` issues; a row containing only
such cells is reported as a failure, not skipped as blank. External
relationships are never opened.

`DefaultLimits` bounds 100,000 data rows, 1024 columns, 32 KiB cells, 1 MiB CSV
records, 64 MiB of input, 128 MiB of decompressed XML, one million shared
strings totalling 64 MiB, 1000 retained failures and 500-row chunks. The ZIP
central directory is checked (at most 4096 parts) before it is read, every
decompressed byte counts against the XML budget, and every XML token (text,
tag, attribute value, comment, CDATA section or processing instruction) is
bounded to eight times the cell limit plus 4 KiB whatever `<` or `>` it
contains, so compression bombs and oversized tokens fail with `fault.Invalid`
instead of exhausting memory. Document type declarations are rejected. Only the shared-string
table is held in memory; rows are never buffered beyond one chunk.

## Contract migration acceptance

`make datatable-contracts` compares the generated manifest, OpenAPI, full and
surface TypeScript entries, shared runtime modules and React/Vue adapters byte
for byte with `testdata/current-contracts`. It checks both the existing client
fixture and a reporting fixture containing Members, Orders, Totals and the
actual guarded query/download routes. Baseline provenance lives beside the
[current artifacts](../../tests/fixtures/consumer/clientcontracts/README.md).
The gate never updates those artifacts automatically; use `make publish-client-contracts`
after reviewing an intentional contract change.

`make typescript-check` also runs reporting codec, type and anonymous HTTP checks.
`make agent-smoke` covers handwritten ORM declarations and row mappings through
real completion, hover and definition requests. Select the existing native tools
as described in the [tooling guide](developer-tooling-and-testing.md).


<a id="recipe-table-routes"></a>
## Register the standard table routes

Use `datatable.Routes` to register a table's query, ticket-export and download
routes in one call. The parent scope stays application-owned. The helper uses
explicit Part A bindings, so generated clients derive their table operations
from the registered endpoints.

<!-- compiled-source: tests/fixtures/consumer/reporting/table_routes.go#datatable-7 -->
```go
return datatable.Routes(group, NumericLevels, config,
	datatable.ReadAccess(read...),
	datatable.ExportAccess(export...),
)
```

Build one configuration per route group and reuse it for the group's tables:

<!-- compiled-source: tests/fixtures/consumer/reports/datatable_routes.go#table-routes-config -->
```go
func TableRoutesConfig(manager *datatable.Manager, guard http.GuardBinding[reporting.Operator], tokens *token.Tokens[reporting.Operator, model.ID[reporting.Operator]], clock clock.Clock, quota http.Middleware) datatable.RoutesConfig[reporting.Operator, reporting.Authority] {
	return datatable.RoutesConfig[reporting.Operator, reporting.Authority]{
		Manager: manager,
		Guard:   guard,
		Authority: func(context.Context, reporting.Operator) (reporting.Authority, error) {
			return reporting.FromGuard(guard.Guard()), nil
		},
		Tickets: datatable.TokenTickets(tokens),
		Clock:   clock,
		Quota:   quota,
	}
}
```

The authority callback retains the route binding's guard. The guard resolves
the actor through the current request scope when authorization or loading runs.

Supply `RoutesConfig[M, A]` with the manager, concrete `GuardBinding[M]` and an
`Authority(context.Context, M) (A, error)` callback. Route guards use `M`; queries
and exports use `A`. Build read and export guard options from your application's
permission and credential-scope declarations. Each access option may appear once;
the helper snapshots its slice. Table authorization still runs for every query
and export.

Exportable tables also require `Clock`, `Quota` and `Tickets[M]`. Construct tickets
from the same model's token binding. Registration rejects disabled tickets or a
redeemer whose source differs from the route guard.

See the [compiled token-ticket adapter](tokens.md#websocket-handshake-tickets).

Query-only tables need no ticket, clock or quota configuration. The framework owns
redemption and the fresh authentication scope, including deferred row loading.

For table ID `reports.numeric_levels`, the helper registers these paths below
your scope. The normal declared status is 200; downloads also support 206
(partial content) and 304 (not modified):

| Method | Suffix | Request | Response |
| --- | --- | --- | --- |
| POST | `/reports.numeric_levels` | `Request` | Canonical numbered page |
| POST | `/reports.numeric_levels/export` | `ExportRequest` | `ExportTicket` |
| GET | `/reports.numeric_levels/download` | `DownloadQuery` | CSV or XLSX file |

Route IDs append the table ID and `.query`, `.export` or `.download` to the scope
ID. Issuance alone receives `CredentialRequests()` and your quota middleware.
It requires secure transport, authorizes the table, validates the bounded request,
and encodes the download URL before issuing a ticket. It checks the URL again with
the issued secret; final rejection can leave an unused expiring ticket.

The download uses `TicketAuthenticated` and enforces the same `ExportAccess`
options as issuance. Its concrete actor passes through `Authority(ctx, actor)`.
It decodes the request, resolves the optional locale and calls
`Table.Download`, which authorizes again when the file opens. Set `WithLocale`
when your authority carries language into deferred row loading. Omitted locale
uses the download request's negotiated language. The route deadline comes from
`Manager.DownloadTimeout()` and covers generation and transfer. HTTP owns file
closure. Filenames use the table ID and the injected clock's UTC timestamp.

The framework generates `ExportRequestJSON`, `ExportRequestValidationFields`,
`DownloadQueryDescriptor` and `ExportTicketJSON` from its transport declarations.
The format field carries no application translation key; keep visual labels in
your UI. For other wire shapes, use the [endpoint constructors and binders](#declare-http-operation-bindings).

Open the issued URL directly. Never append a bearer token or copy the ticket
into an application query DTO. The single-use ticket is bound to the sign-in and
uses the token service's ticket lifetime.

`DownloadQuery` contains format, request and optional locale. The ticket is a
server-issued credential consumed before typed decoding. Follow the
[ticket-route security notes](authentication.md#ticket-authenticated-routes): use
HTTPS, redact query credentials and obtain a fresh ticket for retries and ranges.
