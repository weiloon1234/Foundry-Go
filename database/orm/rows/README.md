# Joins and model scopes

`As[Tag](query, alias)` names one model occurrence. Use distinct tag types and SQL
names for each occurrence, including self joins. Aliases preserve filters, order,
limits, offsets and lazy model scopes inside their original SELECT boundary.

`InnerJoin`, `LeftJoin`, `RightJoin` and `FullJoin` take `On(left, right)` with
matching concrete key/value types and the exact left and right input scopes.
`CrossJoin` has no ON condition. A join chain goes on the left; the right input
is one alias. Duplicate alias names or tag identities fail before execution.
Each join kind has a distinct scope type. Join validation uses the SQL and alias
metadata returned together by the source getter. Promoting a join marker from
another embedded value cannot grant join access to an unwrapped SELECT. Outer
SELECT clauses require an `As` boundary; unknown source shapes fail before SQL.

```go
u := rows.As[userSide](Users.Query(), "u")
o := rows.As[orderSide](Orders.Query(), "o")
j := rows.LeftJoin(u, o, rows.On(
    expr.At(rows.Scope(u), Users.ID),
    expr.At(rows.Scope(o), Orders.BuyerID),
))
userEmail := expr.At(rows.LeftScope(j, rows.Scope(u)), Users.Email)
orderID := expr.AtNullable(rows.NullableRightScope(j, rows.Scope(o)), Orders.ID)
// Map orderID to a declared value.Nullable[model.ID[Order]] row field.
```

`LeftScope` and `RightScope` preserve non-nullable sides. Left joins require
`NullableRightScope`; right joins require `NullableLeftScope`; full joins require
both. `expr.AtNullable` widens a required column. `expr.AtNullableValue` keeps one
nullable layer for an already nullable column. Both retain present-value
comparisons and nullable selected results. `expr.At` accepts only preserved scopes.
Use `LeftNullableScope(chain, nullableScope)` to retain an earlier nullable side
through another join. Nullable scopes cannot convert to preserved scopes.

`rows.Select` maps complete declared rows. Its mappings, predicates and orders
must use the resulting join scope. References to another declaration with the
same Go model or another alias with the same tag fail validation before SQL.
A NULL from an outer join decodes as `value.Null`, never a Go zero value.

## ON conditions

`OnEqual`, `OnNotEqual`, ordered comparisons, nullable ordered variants, `OnLike`,
`OnLikeNullable`, `OnDistinctFrom` and `OnNotDistinctFrom` accept row-phase
`orm/expr` values. Their operands retain independent left and right scopes and
exact result types. Nullable values require matching nullable operands, including
explicit `expr.NullableRow` widening when needed. Selected aggregates cannot enter
ON comparisons. `expr.OrderedScalar` reuses the expression package's ordered
constraint; enums retain the existing row-expression capability rules.

`OnAnd`, `OnOr` and `Not` combine conditions without erasing either side.
`WhereLeft` and `WhereRight` add predicates to ON, with the corresponding side's
scope. On outer joins, those filters preserve unmatched rows. A WHERE filter on
the resulting join can remove those rows. Conditions and derived filter lists are
immutable, and the shared compiler bounds nested SQL and parameters.

## Independent subqueries

`From(modelQuery)` and `FromRows(projectedQuery)` adapt ordinary queries without
adding Query methods. They retain source filters, windows, deferred declaration
checks and model scopes. `Input` and `Scalar` implement `SelectionSource` for
projections and subqueries, but do not implement the join `Source` interface.
Use `As` on a model query to join its filtered or limited SELECT through an alias
boundary. Passing either adapter directly to a join is a compile error.
Locked queries are excluded. Transaction variants wait
for Stage 6c's transaction record/projection adapters.

```go
outer := rows.From(Users.Query())
inner := rows.SelectScalar(
    rows.From(Orders.Query().OrderBy(Orders.TotalCents.Desc()).Limit(1)),
    expr.Select(expr.Value(Orders.TotalCents)),
)
amount := rows.ScalarQuery(outer, inner)
// Map amount with rows.MapSelected to a value.Nullable[int64] row field.
```

`SelectScalar` accepts one selected expression, including an aggregate. Its
`Scalar[S,V]` descriptor is not executable and cannot be made from a general
projected record. `WhereScalar` adds row predicates with the descriptor's source
scope. Existing `SelectValue` remains unchanged for direct scalar reads.

`ScalarQuery` widens an independent scalar's result to `value.Nullable[V]`.
No rows becomes SQL NULL; multiple rows produce PostgreSQL cardinality error
`21000`. The API adds no implicit LIMIT. `ScalarNullableQuery` retains one existing
nullable layer. Passing already nullable input to `ScalarQuery` fails validation,
as with `expr.Nullable`. Row counterparts `ScalarRowQuery` and
`ScalarNullableRowQuery` expose the same result in row comparisons or arithmetic.
An aggregate inside that SELECT stays in the inner phase.

`InQuery(rowValue, scalar)` requires matching exact types, including nullability.
`InNullableQuery` explicitly admits nullable inner values. Neither filters NULLs
nor replaces them with zero values. `expr.Not` retains SQL's three-valued NOT IN
semantics. `ExistsQuery(outer, inner)` accepts model, projected, alias, join and
scalar sources; it retains inner selections, aggregate phases and windows,
including LIMIT zero. It anchors the predicate to the exact outer declaration.
Independent subqueries do not admit outer references; use the explicit
correlation descriptors below.

All these operations use the existing `querycore.Subquery` and
`SubqueryPredicate` compiler shared by relation EXISTS. Deferred source checks
run before execution, and all nested SELECTs share compiler depth, node,
parameter and SQL-size limits.
PostgreSQL FULL JOIN conditions retain its planner restrictions.


## Correlation and LATERAL

`Correlate(outer, inner)` binds an inner SELECT to a declared enclosing scope.
Use `From` for model queries, `ModelScope` for their model fields, or `As`/`Scope`
for aliases. `OuterScope` and `InnerScope` lift fields into
`Correlation[Outer,Inner]`; the nullable variants retain outer-join nullability.
A containing `CorrelatedSource` can serve as the outer scope of another
correlation. Only its visible ancestors and local sources become visible.
Distinct SQL names are required: inner sources cannot shadow enclosing sources.

```go
parent := rows.As[parentSide](Users.Query(), "parent")
child := rows.As[childSide](Orders.Query(), "child")
link := rows.Correlate(parent, child)
parentID := expr.At(rows.OuterScope(link, rows.Scope(parent)), Users.ID)
childScope := rows.InnerScope(link, rows.Scope(child))
buyerID := expr.At(childScope, Orders.BuyerID)
link = link.Where(expr.Equal(buyerID, parentID))
record := rows.SelectCorrelatedRecord(link, childScope).
    OrderBy(expr.At(childScope, Orders.TotalCents).Desc()).Limit(1)
latest := rows.AsLateral[latestSide](record, "latest")
joined := rows.LeftJoinLateral(parent, latest)
amount := expr.AtNullable(
    rows.NullableRightScope(joined, rows.LateralScope(latest)), Orders.TotalCents)
// Map amount into a declared value.Nullable[int64] row field.
```

`SelectCorrelatedScalar` accepts a lifted selected expression. Consume it with
`CorrelatedScalarQuery`, its nullable/row counterparts, `InCorrelatedQuery`,
`InNullableCorrelatedQuery`, or `Exists`. Missing scalar rows become NULL;
multiple rows remain a database cardinality error. Inner aggregates stay in their
own SELECT. Correlated descriptors have `Where`, `OrderBy`, `Limit`, `Offset` and
`Exists`, with no independent execution or ordinary-source conversion.

`SelectCorrelatedRecord` selects one complete preserved inner model.
`ProjectCorrelated` uses the existing row schema and complete mapping validation
for a declared result. `AsLateral` always retains that record inside a derived
SELECT. It grants no ordinary join or execution capability. `CrossJoinLateral`,
`InnerJoinLateral` and `LeftJoinLateral` require the exact preceding outer scope;
right/full lateral joins are excluded. Inner/left joins accept optional typed ON
conditions, defaulting to ON TRUE. Left joins preserve parents when ON rejects a
record or the inner SELECT is empty, and require nullable right scopes.

For a declared projection result, use `rows.At(scope, Schema.Field)` to read its
column with its original value codec. `rows.AtNullable` widens a required field;
`rows.AtNullableValue` retains one nullable layer. Model results continue to use
`expr.At` and its nullable variants. Both paths validate the declaration identity,
including declarations with identical Go types and SQL names.

Visibility, grouping rules, lateral ordering, bound SQL values, declaration checks
and nested-query limits use the existing shared compiler. Ordinary join boundary
checks also apply to the lateral parent. Transaction variants still wait for 6c.
CTE composition will be covered when the Stage 6c CTE API exists; correlated
records currently have no ordinary selection-source capability.

## Complete record adapters

See [complete records and transaction projections](records.md) for `ModelRecord`,
`RecordOf`, `AsRecord`, `SelectRecord` and the transaction-required record family.
These adapters retain complete decoders and provide the shared foundation for
Stage 6c CTE/set/cursor composition and Stage 6b4 transaction joins.

See [CTEs, recursion and set operations](composition.md) for complete-record CTEs,
transaction CTEs, recursive self scopes, and record/scalar set composition.

See [completed-result cursors and read terminals](results.md) for output scopes,
keyset navigation, numbered pages and transaction-only projection locks.

See [transaction composition](transactions.md) for transaction joins, subqueries, correlation and LATERAL.

## Report reads

Use package functions when a report retains a `rows.Query[S, R]`:

```go
q := rows.ReorderUnwindowed(report, name.Asc(), id.Asc())
if err := rows.Preflight(ctx, q); err != nil { return err }
total, err := rows.Count(ctx, tx, q)
page, err := rows.Paginate(ctx, tx, q, orm.Page(1, 20))
more, err := rows.SimplePaginate(ctx, tx, q, orm.Page(1, 20))
err = rows.Each(ctx, tx, rows.Limit(q, 10001), writeCSV)
```

`Compile(q)` returns an immutable `orm.Statement` for inspection. `Preflight`
uses the executing request context, resolving context scopes without opening a
connection. Execution uses its own current context and the supplied executor.
`Count` wraps the complete selection, preserving grouping, HAVING, DISTINCT and
existing result windows. Pagination requires explicit ordering and no outer
limit/offset. Supply a unique final tie-breaker. Use a repeatable-read transaction
when the count and page must share one snapshot.

`ReorderUnwindowed` replaces only outer ordering. It rejects outer LIMIT/OFFSET,
DISTINCT ON and an empty order list; nested windows and scopes survive.
`Each` retains one decoded result at a time. A callback error, panic, Goexit,
decoding failure or cancellation closes the rows; completed callbacks cannot be
undone. It does not run complete-model retrieval hooks, matching projection reads.
Exports should set their own row limit and stop on the first excess row.

Scalar SELECTs keep their subquery boundary when composed with `OrderScalar`,
`LimitScalar` and `OffsetScalar`. These package functions also accept scalars
selected from CTE aliases. Ordering accepts row and selected orders in the same
scope; negative bounds and foreign declarations fail normal compile/preflight
validation. No implicit limit hides scalar cardinality errors.

Use `HavingCorrelatedRecord` and `DistinctCorrelatedRecord` before `AsLateral`,
and `DistinctOnCorrelatedScalar` before a correlated scalar/EXISTS operation.
HAVING takes lifted group predicates; DISTINCT ON takes lifted row keys and keeps
PostgreSQL's ordering validation. The resulting descriptors retain their parent,
input and result types and do not acquire independent execution capabilities.

`GroupByScalar` and `HavingScalar` add row keys and group predicates within a
scalar SELECT. `DistinctScalar` deduplicates its selected value after grouping;
`DistinctOnScalar` takes row keys, including keys absent from the selected output.
`DistinctOnScalarValues` accepts selected keys while retaining group/window
validation. Order by the DISTINCT ON keys first. These clauses preserve the selected codec,
nullability and subquery boundary. They never impose a scalar cardinality limit.

`DistinctRecord`, `DistinctSet` and `DistinctValues` deduplicate complete record,
record-set and value-set results. They preserve inner input SELECTs and retain the
ordinary read-only result family. Existing bounds and ordering remain in place;
NULL equality and selected-order validation follow the shared PostgreSQL compiler.

Correlated scalar keys use `GroupByCorrelatedScalar` for lifted row grouping and
`DistinctOnCorrelatedScalarValues` for selected keys. Use `OrderCorrelatedScalar`
when ordering by aggregates or windows; its leading orders must match the DISTINCT
ON keys. These functions preserve the enclosing scope and inner limit/offset.

`DistinctOnValueSet` accepts selected keys in the combined `Set[V]` scope. For
example, `rows.DistinctOnValueSet(values, expr.Select(values.Value()).Key())`
retains the scalar codec, including nullability. Group keys cannot be substituted
for selected keys, and keys from either input query cannot enter the combined
scope. Both families reuse the ordinary rows clause and compiler validation.

Derived result fields can be aggregated without losing their declaration ownership:
`expr.SumRow(rows.At(scope, Totals.Amount))` and `expr.SumNullableRow` return nullable
exact decimals. The float, interval, min/max and count families keep their existing
result rules. Only declared ordered fields grant ordered aggregate input; enums,
computed parameters and foreign declarations fail validation. The original
`expr.SumAt(scope, modelColumn)` API remains unchanged.

`DistinctOnRecord(record, keys...).OrderBy(...)` keeps keys and ordering in the
original input scope, including joined fields outside the selected complete record.
It remains read-only and uses the shared ordering-prefix validation.

`CompileCursor` and `PreflightCursor` validate completed-output cursor keys and
canonical ordering without I/O. `CompileValueCursor` and `PreflightValueCursor`
provide the same boundary for scalar cursors. Compilation adds no page boundary
or lookahead limit; preflight uses the executing context, including current global
scopes. Invalid computed keys are rejected here, before pagination is invoked.
