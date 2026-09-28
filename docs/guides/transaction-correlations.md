# Transaction-required correlation and lateral joins

Use these APIs when an inner query both depends on a parent row and needs row locks. The parent remains part of the type contract; the final query also requires `*database.Tx`. They extend [transaction query composition](transaction-query-composition.md) and share the ordinary [correlation](model-correlations.md) and [lateral](lateral-joins.md) compiler.

The examples use the `Record` model and `Summary` projection declared in the transaction guide. The [consumer acceptance](../../tests/fixtures/consumer/transactionqueries/correlations_postgres_test.go) executes these APIs against independent PostgreSQL transactions.

## Declare the parent and child

```go
type parentAlias struct{}
type childAlias struct{}
type chosenAlias struct{}

parent := query.AsTransaction[parentAlias](
    query.TransactionOf(QueryRecords()), "parent")
child := query.AsTransaction[childAlias](
    query.TransactionOf(QueryRecords()), "child")

link := query.TransactionCorrelate(parent, child)
parentFields := RecordFieldsAt(query.OuterScope(link, parent.Scope()))
childScope := query.InnerScope(link, child.Scope())
childFields := RecordFieldsAt(childScope)
link = link.Where(query.Greater(childFields.ID, parentFields.ID))
```

`OuterScope`, `InnerScope` and their nullable variants work with both correlation families. Fields lifted into `link` belong to its `TransactionCorrelation[Outer, Inner]` scope. A plain parent or child predicate cannot be substituted for that combined scope. Parent filters still belong to the parent query. Distinct aliases prevent a child source from shadowing its parent.

A transaction correlation can be the outer scope of another correlation. Its visible parent and child fields remain explicit; each nested level retains its own required parent and lock clauses. Use `OuterNullableScope` or `InnerNullableScope` when the supplied record is already nullable.

## Select a locked correlated result

```go
selected := ProjectTransactionCorrelatedSummary(link).
    SelectID(childFields.ID.Value()).
    SelectName(parentFields.Name.Value()).
    Query().
    OrderBy(childFields.ID.Asc()).Limit(1).
    ForUpdate().Of(childScope).SkipLocked()

lateral := query.AsTransactionLateral[chosenAlias](selected, "chosen")
joined := query.TransactionLeftJoinLateral(parent, lateral)
fields := SummaryNullableFieldsAt(
    query.NullableRightScope(joined, lateral.Scope()))

ids, err := query.SelectTransactionValue(joined, fields.ID.Value()).All(ctx, tx)
```

`ids` is `[]value.Nullable[int64]`. Each parent requests its own first available child. An unmatched parent remains in a left lateral result with NULL child fields. The child projection can combine explicitly scoped parent and child values.

Use `SelectTransactionCorrelatedRecord(link, childScope)` to select the complete inner model with its original decoder. Generated projections also offer `SelectTransactionCorrelatedSummary(link, selection)` as the struct-selection alternative. These correlated builders cannot execute independently, become an ordinary source, or become an uncorrelated transaction CTE/value query.

`TransactionCrossJoinLateral` and `TransactionInnerJoinLateral` retain matching records; empty inner results remove the parent. Inner and left forms accept optional typed ON conditions. Complete record selection from a lateral result requires a preserved right scope, available with inner/cross joins. Nullable sides use declared projections or nullable values.

Conditions inside the correlated query, ON conditions and final result filters occupy distinct query phases. The inner limit chooses candidates per parent; an outer limit bounds the completed result. PostgreSQL can optimize predicate evaluation, so returned rows and acquired locks are not interchangeable counts.

## Correlated scalars and membership

```go
values := query.SelectTransactionCorrelatedValue(link, childFields.ID.Value()).
    OrderBy(childFields.ID.Asc()).Limit(1).ForShare()

scalar := query.TransactionCorrelatedScalarQuery(values)
ids, err := query.SelectTransactionValue(parent, scalar).All(ctx, tx)
```

An empty inner result becomes NULL; multiple rows preserve PostgreSQL's cardinality error. No scalar helper adds an implicit limit. `TransactionCorrelatedScalarNullableQuery` keeps one nullable layer for an already nullable input. The corresponding `TransactionCorrelatedScalarRowQuery` and `TransactionCorrelatedScalarNullableRowQuery` helpers allow parent row predicates.

`values.Exists()`, correlated records' `Exists()` and the correlation's own `Exists()` return predicates owned by the declared parent. `TransactionInCorrelatedQuery(parentValue, values)` accepts a field or computed row value with the exact result type and parent scope. `TransactionInNullableCorrelatedQuery` explicitly admits nullable inner results and preserves SQL's three-valued membership semantics.

## Lock policies and failures

Both correlated record and value queries support `ForUpdate`, `ForNoKeyUpdate`, `ForShare`, `ForKeyShare`, `Of`, `LockRows`, `NoWait`, `SkipLocked` and `Wait`. Locks belong to the inner SELECT; locks already declared inside its sources remain in their own SELECTs. A strength method replaces this SELECT's clauses. Deriving a policy or predicate leaves its source unchanged.

Wait modifiers and `Of` require an existing lock. Empty per-input clauses, an outer parent used as an inner lock target, and grouped/distinct/window/set lock targets fail validation before execution. The same existing lock-target and combined-work bounds apply. Database contention and cardinality errors retain their SQLSTATE and require transaction/savepoint recovery as described in [row locking](row-locking.md).

The public source interfaces retain both transaction and outer ownership. A lateral source only joins after its declared parent. The shared compiler additionally rejects lock-carrying expressions through ordinary low-level generic query paths. Runtime validation still checks concrete alias visibility, declared columns, query bounds and legal SQL phases. The only executable result belongs to the enclosing transaction-required query.
