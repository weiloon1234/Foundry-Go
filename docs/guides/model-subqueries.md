# Typed subqueries and derived records

Foundry composes model, projection and single-value queries through the same SELECT AST. Each SELECT has its own input scope. IDs, enums and nullable result types survive composition as ordinary Go types, with generated methods visible to gopls. The [independent consumer fixture](../../tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go) exercises these APIs against PostgreSQL; its [report structs](../../tests/fixtures/consumer/reports/subquery_projections.go) remain handwritten domain declarations.

## Use a report as a query source

Declare and select a complete [projection record](model-projections.md), then alias it with `As`:

```go
o := models.OrderFields()
totals := reports.ProjectBuyerTotals(models.QueryOrders()).
    SelectBuyerID(o.BuyerID.Value()).
    SelectTotal(o.TotalCents.Sum().Value()).
    SelectOrders(query.Count[models.Order]().Value()).
    SelectAverage(o.TotalCents.Avg().Value()).
    Query().GroupBy(o.BuyerID.Group())

type totalsAlias struct{}
source := query.As[totalsAlias](
    totals.Having(query.Count[models.Order]().Gte(2)).
        OrderBy(query.Count[models.Order]().Desc()).Limit(1),
    "totals",
)
fields := reports.BuyerTotalsFieldsAt(source.Scope())
amounts, err := query.SelectValue(source, fields.Total.Value()).
    Where(fields.Total.Gt(decimal.FromInt64(2))).All(ctx, db)
```

The derived SELECT retains grouping, HAVING, ordering and its window. The outer WHERE operates on the declared report's output columns. Generated output aliases, such as `contact_email`, are resolved automatically. `amounts` has type `[]value.Nullable[decimal.Decimal]`; a decimal field does not accept float values or text operators.

`BuyerTotalsFields()` describes output mapping destinations. `BuyerTotalsFieldsAt(scope)` describes queryable fields after introducing that record as a source. The latter requires a `RecordScope[Input, BuyerTotals]`: a scope belonging to a different record or model cannot be substituted. `ModelScope` remains the model-facing alias for the shared record scope type.

`As` accepts complete model queries, declared projection queries and [typed CTEs](model-ctes.md). It rejects missing mappings, invalid aliases, eager-loading clauses and loading limits before execution. Its sealed `RecordQuerySource[Result]` contract preserves the output record separately from the original query's input scope. Do not use handwritten field constructors to bypass generated ownership checks; they are explicit metadata declaration boundaries, not schema inference.

## Join derived records

The same [join and scope helpers](model-joins.md) work with derived reports. For example, join the totals above onto users:

```go
type buyerAlias struct{}
buyers := query.As[buyerAlias](models.QueryUsers(), "buyer")
joined := query.LeftJoin(buyers, source,
    query.On(models.UserFieldsAt(buyers.Scope()).ID, fields.BuyerID))

buyer := models.UserFieldsAt(query.LeftScope(joined, buyers.Scope()))
stats := reports.BuyerTotalsNullableFieldsAt(
    query.NullableRightScope(joined, source.Scope()))
report := reports.ProjectUserOrderStats(joined).
    SelectID(buyer.ID.Value()).SelectEmail(buyer.Email.Value()).
    SelectTotal(stats.Total.Value()).SelectOrders(stats.Orders.Value()).
    Query().OrderBy(buyer.Email.Asc())
```

The outer join makes `Orders` nullable. `Total`, already nullable, retains one nullable layer. Generated nullable accessors require `NullableRecordScope`; ordinary fields cannot erase the missing-row possibility. Model IDs keep their model ownership inside nullable fields.

A complete projection of a joined query may itself be aliased and joined again, including as the right input. This provides a declared result boundary around a nested join tree. The outer query sees only that record's columns; aliases and model fields inside it are not visible. Independent SELECT scopes may reuse SQL alias names. Within one scope, duplicate names and repeated typed alias identities still fail.

## Select one typed value

`SelectValue(source, expression)` returns `ValueQuery[Input, Value]`. It retains the input scope through `Where`, `GroupBy`, `Having` and `OrderBy`, and provides `Limit`, `Offset`, `Compile`, `Each`, `All`, `First`, `RequireFirst`, `Count` and `Exists` through the existing projection runtime. It exposes no model writes or eager loading.

```go
u, o := models.UserFields(), models.OrderFields()
buyers := query.SelectValue(models.QueryOrders(), o.BuyerID.Value()).
    Where(o.TotalCents.Gte(2))
users, err := models.QueryUsers().Where(u.ID.InQuery(buyers)).All(ctx, db)
```

`ValueQuerySource[Value]` is the sealed single-column boundary consumed by `InQuery`. The input scope may differ, but the concrete value type must match. An order's own ID cannot be used in membership against a user ID. The query remains nested SQL; Foundry does not fetch a list first or interpolate values.

`InNullableQuery` explicitly accepts a nullable output of the same base type:

```go
introducers := query.SelectValue(models.QueryUsers(), u.IntroducerID.Value())
predicate := u.ID.InNullableQuery(introducers)
```

`predicate.Not()` follows SQL NOT IN behavior. NULL inside the subquery can make non-matches unknown, so negation does not mean every other row. Filter the inner nullable field with `IsNotNull` when the desired set excludes NULL. Empty IN selects no rows; negated empty IN includes all rows. These follow PostgreSQL's [subquery comparison semantics](https://www.postgresql.org/docs/18/functions-subquery.html).

## Existence and scalar values

`ExistsQuery(outer, inner)` creates an uncorrelated predicate in the outer scope. It accepts model, projection, value, alias and joined sources, preserving the inner result's grouping, HAVING and window. The `outer` argument anchors the Go scope; use the actual outer query to apply its filters.

```go
eligible := models.QueryUsers().Where(
    query.ExistsQuery(models.QueryUsers(), buyers))
```

This condition has the same result for every outer row: it asks whether the inner query returns any rows. A scalar `COUNT(*)` query over empty input still has one result row, so EXISTS is true unless HAVING or a window removes that row.

`ScalarQuery(outer, inner)` turns a single-column query into `Expression[Outer, value.Nullable[Value]]`. An empty inner result becomes NULL; more than one row returns PostgreSQL's cardinality error. Foundry does not silently insert a limit. Specify ordering and `Limit(1)` only when choosing one row is the intended behavior. See PostgreSQL's [scalar subquery rules](https://www.postgresql.org/docs/18/sql-expressions.html#SQL-SYNTAX-SCALAR-SUBQUERIES).

```go
last := buyers.OrderBy(o.TotalCents.Desc()).Limit(1)
expression := query.ScalarQuery(models.QueryUsers(), last)
```

This expression can fill a nullable user-ID field in a declared projection or be selected directly with `SelectValue`. `ScalarNullableQuery` accepts an already nullable value query and preserves a single nullable layer. Present values retain their codec; absent rows and SQL NULL are represented as `value.Null`. A nullable scalar cannot populate a non-nullable result field. Scalar subqueries may also be ordered as typed expressions.

For row calculations and conditions, use `ScalarRowQuery` or `ScalarNullableRowQuery`. Their inner SELECT retains its own aggregate/window phase, while the outer row value composes with typed comparisons and calculations. [Value comparisons and joins](value-comparisons-and-joins.md) also document the corresponding explicit-correlation helpers. These constructors preserve the same NULL and cardinality behavior and add no implicit limit.

## Failure and resource behavior

Compilation validates each isolated SELECT scope and shares parameter numbering, expression budgets and nesting limits across the complete statement. Invalid source/mapping declarations return no compiled statement. Subqueries are parameterized, with no raw SQL string composition in application code.

Execution uses the caller's context and executor. `All` discards collected output on failure; `Each` closes rows on callback error, decoding failure or cancellation. Callback effects already performed remain the caller's responsibility. Select a bounded window or stream large outputs. Generated report and single-value decoders validate stored enums and return owned values.

A server-side scalar cardinality error aborts the current PostgreSQL transaction scope. Use a savepoint if the outer transaction must continue after such an error. Foundry preserves SQLSTATE metadata through its ordinary database error contract. A local decoding error closes the stream without claiming a server transaction failure.

The ordinary APIs above isolate their SELECT scopes and reject accidental outer-column references. [Explicit correlated subqueries](model-correlations.md) retain outer ownership through existence, membership and scalar expressions, including nested and nullable join scopes. [Relationship filters](model-relationship-filters.md) generate that correlation from existing typed relationship declarations. [Milestone 06](../../blueprint/06-relations-and-advanced-queries.md) records delivered advanced-query APIs and remaining contracts.
