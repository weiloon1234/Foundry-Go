# Typed model queries

<a id="agent-entry"></a>
## Agent entry

- Read: [Bounded typed queries](#recipe-read-models)
- Filter: [Typed expressions](#recipe-filter-expressions)
- Write: [Create and update](model-writes.md#recipe-write-models)
- Lock: [Change unique columns](model-writes.md#recipe-lock-write)
- Load: [Relations](model-relations.md#recipe-load-relations)
- Project: [Typed row declarations](model-projections.md#recipe-declare-rows)
- Search: [Trigram queries](trigram-search.md#recipe-trigram-search)

## Overview

Further reading: [Transaction composition](transaction-query-composition.md).

Declare a model once with `//foundry:model` and generate its table; `Table.Query()` returns
an immutable `orm.Query[M]`. Models and returned collections remain ordinary Go
structs and slices. The [model declarations](../../tests/fixtures/consumer/models/models.go)
and [migration examples](../../tests/fixtures/goway/migration/examples_test.go)
show reads using public APIs.

<a id="recipe-read-models"></a>
## Query construction and execution

`Where`, `OrderBy`, `Limit` and `Offset` derive queries without I/O. Conditions
retain model and value ownership. Execution takes an explicit context and executor;
a pool, transaction or session stays owned by its caller. Keep work on a single
transaction/session sequential.

| Terminal | Result and behavior |
| --- | --- |
| `orm.All(ctx, executor, query)` | Complete `[]M`; no partial collected result on failure |
| `orm.First(ctx, executor, query)` | `value.Optional[M]`; no match is absence, not a zero model |
| `orm.Require(ctx, executor, query)` | Complete `M`, or `database.NotFound` |
| `orm.Count(ctx, executor, query)` | Selected-window count without model hydration |
| `orm.Exists(ctx, executor, query)` | Whether the selected window has a row |
| `orm.Each(ctx, executor, query, callback)` | Bounded iteration with cleanup on all exits |
| `orm.Compile(query)` | Redacted `orm.Statement`, no I/O |

Use `rows.Read`/`ReadScalar`/`ReadRecord` and the package terminals for
[projections and other read families](model-projections.md#execution-and-failures).
Those read handles call their required-first terminal `RequireFirst`.

`All` retains its result in memory. Use a limit, stream, or [bounded chunks](model-chunks.md)
for large results. Ordinary in-memory Go slice operations issue no SQL; sorting a
page after reading it does not order the full matching dataset.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database`, `database/orm`, `database/orm/expr`, `database/orm/rows`, `model`, `value`.

`models.Order` is the application's model and `models.Orders` its generated table. The three terminals read a filtered list, one order by key and one required order:

<!-- compiled-source: tests/fixtures/goway/migration/guide_examples.go#query-and-find -->
```go
func largeOrders(ctx context.Context, db database.Executor) ([]models.Order, error) {
	return orm.All(ctx, db, models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100)))
}

func findOrder(ctx context.Context, db database.Executor, id model.ID[models.Order]) (value.Optional[models.Order], error) {
	return orm.Find(ctx, db, models.Orders.Table, id)
}

func requireOrder(ctx context.Context, db database.Executor, id model.ID[models.Order]) (models.Order, error) {
	return orm.Require(ctx, db, models.Orders.Query().Where(orm.Eq(models.Orders.ID, id)))
}
```

## First rows and typed identities

Filter with the declared primary-key column to retrieve a typed identity.
`orm.Find(ctx, executor, table, key)` is the direct key lookup; natural keys keep their declared type.
`First` and `Require` preserve filters/offset and respect a zero limit. Supply
ordering when row choice matters. Missing and database failure remain different
outcomes through the framework's typed errors.

<a id="recipe-filter-expressions"></a>
## Typed predicates and expression ordering

Methodless column handles use one generic function per operation, such as
`orm.Eq(column, value)` and `orm.Gt(column, value)`. A type constraint on the
column's kind decides which columns each function accepts, so a wrong pairing
fails to compile. Concrete column types are kept; values are not boxed and
nothing is dispatched at run time. Each family preserves its declared equality/membership,
NULL checks, ordered comparisons or text patterns. Enums remain equality-only;
model IDs retain their model owner. Empty membership is false. NULL comparisons
follow SQL three-valued logic; use explicit NULL checks for absence. Empty
`orm.And` is true and empty `orm.Or` is false. Empty `NotIn` matches everything;
nonempty `NotIn` does not include NULL operands. Combine it with `IsNull` when
NULL rows should match.

`Contains`, `StartsWith` and `EndsWith` escape pattern metacharacters for literal
search. `Like`/`ILike` accept SQL patterns. Values are bound, never interpolated.
Use `expr.Where` and `expr.OrderBy` to apply computed row predicates/orders; adapt
a model predicate with `expr.ModelPredicate` when composing expressions. Pass
the column directly when an expression API accepts one, for example
`expr.Value(Users.Email)`. Its column family, model, value and role constraints
are preserved.

Use `expr.TextRowValue(v)` or `expr.TextNullableRowValue(v)` to compare a
computed `RowValue`, including a correlated scalar, as text. These lifts preserve
the exact string type, scope, codec and SQL NULL semantics without applying
`lower()`. Stored column metadata must grant text capability; string enums are
rejected. Use `TextRow` or `TextNullableRow` for stored text columns.

`Sample` is the application's model with a nullable text `Note` and an integer `ID`, `Samples` its generated table and `Label` its string-backed text type. `NoteSide` is an empty tag type naming the aliased source. `NoteSearch` finds samples whose note, read through a correlated scalar, contains the term:

<!-- compiled-source: tests/fixtures/consumer/scalarqueries/guide_calculations.go#computed-text -->
```go
type NoteSide struct{}

func NoteSearch(term Label) orm.Query[Sample] {
	source := rows.From(Samples.Query())
	lookup := rows.As[NoteSide](Samples.Query(), "note_lookup")
	correlation := rows.Correlate(source, lookup)
	outer := rows.OuterScope(correlation, rows.ModelScope(Samples.Query()))
	inner := rows.InnerScope(correlation, rows.Scope(lookup))
	scalar := rows.SelectCorrelatedScalar(correlation,
		expr.AsSelected(expr.Row(expr.At(inner, Samples.Note)))).
		Where(expr.Equal(expr.At(inner, Samples.ID), expr.At(outer, Samples.ID))).Limit(1)
	text := expr.TextNullableRowValue(rows.CorrelatedScalarNullableRowQuery(scalar))
	return expr.Where(Samples.Query(), expr.NullableTextIContains(text, term))
}
```

`NullableTextIContains` escapes literal pattern characters and ignores case.
`NullableTextLike` accepts a SQL pattern and preserves database case semantics.
For datatable filters, pass the lifted value to `expr.NullableTextFilter` and
`datatable.NullableWhere`; enable SQL patterns explicitly with `AllowLike()`.

[Conditional values](conditional-expressions.md), [arithmetic/text](scalar-calculations.md),
[temporal values](temporal-calculations.md) and [JSON paths](json-models.md) preserve
scope, phase and codecs. Aggregate/window selections do not become row predicates.

## Aggregates and complete hydration

Count/Exists observe filters and the selected limit/offset. Use an unpaginated
source for a total. Counts omit irrelevant ordering but retain ordering needed
by DISTINCT ON or a window. Separate statements need appropriate isolation for
one snapshot.

Complete model reads select declared persisted columns and decode fresh complete
values. Invalid enums, NULLs or out-of-range values fail decoding. Retrieval
callbacks apply to complete model reads; DTO/scalar projections have no model
retrieval pipeline. Relations and [aggregate slots](model-aggregates.md) use
explicit loading; ordinary field access never queries.

## Diagnostic and resource boundaries

`orm.Compile` validates declarations, scopes and bounded SQL construction without
connecting. Explicit `Statement.SQL()` and `Arguments()` disclose SQL and values;
argument slices are copied. Ordinary formatting redacts retained bound data,
including nested wrappers. Structural identifiers/windows and hydrated results
are not secrets covered by that redaction contract.

The shared compiler owns identifier, expression, nesting and parameter bounds.
Declaration metadata is checked once and reused; no per-model generated query
engine is required. See the [diagnostic contract](../../database/orm/README.md#diagnostic-redaction)
and [ORM performance](orm-performance.md). [Writes](model-writes.md),
[relations](model-relations.md) and [pagination](model-pagination.md) share these
generated table declarations.
