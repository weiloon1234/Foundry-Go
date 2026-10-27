# Transaction joins, subqueries and LATERAL

Use `TransactionOf(ModelRecord(query))` for an ordinary model or projection record,
or `LockedRecord(query.ForUpdate())` to retain a model lock. Name each input with
`AsTransaction[Tag](record, "alias")`; use `TransactionScope(alias)` for its fields.
`TransactionInnerJoin`, `TransactionLeftJoin`, `TransactionRightJoin`,
`TransactionFullJoin` and `TransactionCrossJoin` preserve each input's filters,
windows and locks at its alias boundary. The existing `LeftScope`, `RightScope`,
`NullableLeftScope`, `NullableRightScope` and `LeftNullableScope` functions retain
the join's transaction scope and nullability. Nullable records require a declared
projection with nullable fields.

```go
parents := rows.AsTransaction[parentTag](
    rows.TransactionOf(rows.ModelRecord(Users.Query())), "parents")
children := rows.AsTransaction[childTag](
    rows.TransactionOf(rows.ModelRecord(Orders.Query())), "children")
c := rows.TransactionCorrelate(parents, children)
outer := rows.TransactionOuterScope(c, rows.TransactionScope(parents))
inner := rows.TransactionInnerScope(c, rows.TransactionScope(children))
c = c.Where(expr.Equal(expr.At(inner, Orders.BuyerID), expr.At(outer, Users.ID)))
record := rows.SelectTransactionCorrelatedRecord(c, inner).
    OrderBy(expr.At(inner, Orders.ID).Desc()).Limit(1).
    ForUpdate().Of(rows.Target(inner)).SkipLocked()
latest := rows.AsTransactionLateral[latestTag](record, "latest")
joined := rows.TransactionLeftJoinLateral(parents, latest)
ids := rows.SelectTransactionValue(joined, expr.Row(
    expr.AtNullable(rows.NullableRightScope(joined,
        rows.TransactionLateralScope(latest)), Orders.ID)).Value())
items, err := ids.All(ctx, tx)
```

Only cross, inner and left LATERAL joins are supported. The right input must
reference exactly its preceding parent, and ON expressions retain their sides.
Correlated sources, scalar results and records have no independent execution or
ordinary-source conversion. Nested correlations explicitly lift enclosing scopes.
Use `ProjectTransactionCorrelated` for a complete declared projection.

`TransactionScalarRowQuery`, `TransactionScalarNullableRowQuery`, `TransactionInQuery`, `TransactionInNullableQuery` and
`TransactionExistsQuery` reuse the shared subquery compiler. For example,
`rows.TransactionScalarOf(selected).ForShare().NoWait()` adds a lock while
retaining a scalar's codec. `Of` and `LockRows` accept preserved targets in that
scalar's input scope. `TransactionSubqueryOf` explicitly lifts an ordinary SELECT
for EXISTS composition. `TransactionValueOf(rows.Values(selected))` lifts an
ordinary scalar, retaining its codec and clauses. Missing scalar rows become NULL; no implicit LIMIT is
added, and multiple rows retain PostgreSQL's cardinality error.

The `TransactionCorrelatedScalarRowQuery` family and `TransactionInCorrelatedQuery`
retain the required enclosing scope. Call `.Value()` on a scalar row expression
to use its selected result. Correlated selections support inner ordering,
grouping, HAVING and locks. Correlated records also support DISTINCT variants.
Lock policy changes without an existing lock fail validation. Results execute
through the caller's `*database.Tx`; no pool or session adapter is introduced.

Transaction requirements remain attached to SQL sources through aliases, joins
and subqueries, even without a row lock. The shared compiler rejects ordinary
execution before issuing SQL; embedding a transaction scope marker in an ordinary
model does not grant transaction execution.

Transaction and locked query phases also have distinct private layouts, so an
explicit Go conversion cannot turn them into ordinary query wrappers. Transaction
requirements are stamped at construction, before composition.

The persistent conversion guard enumerates exported types in `orm`, `orm/expr`,
`orm/rows`, including pairs across packages. Private zero-size
fields separate execution, row/selected expression, relation cardinality,
nullability and write-plan phases. Intentional same-phase conversions are listed
individually with reasons; a new mutually convertible pair requires review.

The guard matches nested and concrete generic arguments, including declarations
with different parameter counts, then checks both conversions with `go/types`.
Aggregate wrappers, JSON field capabilities, model-reading lookups and insert/update
mappings have distinct private phases. Input-carrier layout exceptions are marked
`pending: lane B roles slice`; these are tracked gaps until the typed roles land.
