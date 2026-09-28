# Typed lateral joins

A lateral join evaluates a right-hand query using the current preceding row. This supports queries such as the two latest orders per user in one database statement. It uses the same explicit [correlation scopes](model-correlations.md), generated fields and complete record decoders as other Foundry queries. See [PostgreSQL lateral semantics](https://www.postgresql.org/docs/18/queries-table-expressions.html#QUERIES-LATERAL).

When the correlated SELECT needs its own row locks, use [transaction-required correlation and lateral joins](transaction-correlations.md). They share these scope and join semantics while retaining the enclosing transaction requirement.

## Complete models per parent

```go
type buyerAlias struct{}
type orderAlias struct{}
type latestAlias struct{}

buyers := query.As[buyerAlias](models.QueryUsers(), "buyer")
orders := query.As[orderAlias](models.QueryOrders(), "candidate")
link := query.Correlate(buyers, orders)
buyer := models.UserFieldsAt(query.OuterScope(link, buyers.Scope()))
orderScope := query.InnerScope(link, orders.Scope())
order := models.OrderFieldsAt(orderScope)
link = link.Where(query.Equal(order.BuyerID, buyer.ID))

records := query.SelectCorrelatedRecord(
    link, orderScope,
).OrderBy(order.TotalCents.Desc(), order.ID.Asc()).Limit(2)

latest := query.AsLateral[latestAlias](records, "latest")
joined := query.CrossJoinLateral(buyers, latest)
result, err := query.SelectRecord(
    joined, query.RightScope(joined, latest.Scope()),
).All(ctx, db)
```

`result` is `[]models.Order`, hydrated through the original generated decoder. The limit applies independently to each buyer. Multiple matching right rows retain their multiplicity; an empty right result removes that buyer from a cross or inner lateral join. Inner ordering chooses the per-parent records; add final ordering when the completed result order matters.

`SelectCorrelatedRecord` accepts complete preserved inner records, including declared projections. It cannot hydrate a missing outer-join side into an incomplete model.

## Preserve parents without matches

```go
joined := query.LeftJoinLateral(buyers, latest)
nullable := models.OrderNullableFieldsAt(
    query.NullableRightScope(joined, latest.Scope()),
)
totals, err := query.SelectValue(joined, nullable.TotalCents.Value()).All(ctx, db)
```

`totals` is `[]value.Nullable[int64]`. Use a declared projection to include buyer fields beside nullable order fields. A left lateral join retains a buyer when the right query yields no rows or its ON conditions reject every candidate. `LeftNullableScope` preserves an already-nullable field through a later join; `OuterNullableScope` brings it into another explicit correlation.

`InnerJoinLateral` and `LeftJoinLateral` accept optional typed `JoinOn` conditions. Omitting them means `ON TRUE`; multiple conditions combine with AND. Use the existing `On`, computed `OnEqual`/`OnLess`, and `WhereLeft`/`WhereRight` contracts. Conditions inside the correlated query run before its limit, ON runs against those selected candidates, and a final result WHERE can remove preserved parents. These placements are deliberately distinct.

## Declared reports

Each generated projection has `ProjectCorrelatedReport(source)` and `SelectCorrelatedReport(source, selection)` counterparts to its ordinary selectors. The fluent builder infers both scopes and checks every output value:

```go
type statsAlias struct{}

summary := reports.ProjectCorrelatedOrderSummary(link).
    SelectTotal(order.TotalCents.Sum().Value()).
    SelectCount(order.ID.Count().Value()).Query()

stats := query.AsLateral[statsAlias](summary, "stats")
joined := query.CrossJoinLateral(buyers, stats)
```

The filtered `link` from the first example includes only that buyer's orders. A plain aggregate produces one row even for no matching orders: count is zero and sum is NULL. HAVING can remove that aggregate row.

Correlated records retain typed `Where`, `GroupBy`, `Having`, `OrderBy`, `Distinct`, `DistinctOn`, `DistinctOnValues`, `Limit` and `Offset`. Window expressions use `WindowFor(link)` and retain their own SELECT phase. `Exists` returns a predicate for the required outer scope. Complete joined results reuse ordinary pagination, CTEs, sets and explicit transaction-scoped row locking. An outer lock on a chosen preserved source uses the existing `ForUpdate().Of(scope)` contract.

## Ownership and validation

`CorrelatedRecordQuery` cannot execute independently, become an ordinary alias/CTE, or enter an ordinary join. `AsLateral` retains its outer type, and lateral join functions require that same left input type. Right/full correlated lateral joins are not exposed because the preserved right side cannot depend on a missing left row.

Runtime validation checks concrete alias names, complete metadata, source visibility, expression bounds and windows before executor access, including zero-limit reads. A lateral input can reference its declared preceding sources; its own alias and future joins cannot satisfy that requirement. Ordinary derived sources continue to reject accidental correlation. Inner aggregates over only outer fields remain invalid because PostgreSQL would assign them to a different query level.

Generation and execution share existing mapping validation, AST traversal, parameter binding, CTE discovery and decoders. Applications provide domain models, declared report structs and typed predicates; they do not construct SQL column strings or a second schema.

The [independent consumer fixtures](../../tests/fixtures/consumer/lateralqueries) exercise these APIs. The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records verification evidence and remaining milestone work.
