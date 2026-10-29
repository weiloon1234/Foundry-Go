# Typed model queries

<!-- compiled-examples -->

Declare a model's table and columns once with `orm.Define`; `Table.Query()` returns
an immutable `orm.Query[M]`. Models and returned collections remain ordinary Go
structs and slices. The [handwritten declarations](../../tests/fixtures/consumer/models/orm.go)
and [migration examples](../../tests/fixtures/goway/migration/examples_test.go)
show reads using public APIs.

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

## First rows and typed identities

Filter with the declared primary-key column to retrieve a typed identity.
`orm.Find(ctx, executor, table, key)` is the direct key lookup; natural keys keep their declared type.
`First` and `Require` preserve filters/offset and respect a zero limit. Supply
ordering when row choice matters. Missing and database failure remain different
outcomes through the framework's typed errors.

## Typed predicates and expression ordering

Methodless column handles use per-kind functions, such as `orm.TextEq(column, value)`
and `orm.NumberGt(column, value)`. Each family preserves its declared equality/membership,
NULL checks, ordered comparisons or text patterns. Enums remain equality-only;
model IDs retain their model owner. Empty membership is false. NULL comparisons
follow SQL three-valued logic; use explicit NULL checks for absence. Empty
`orm.And` is true and empty `orm.Or` is false. Empty `NotIn` matches everything;
nonempty `NotIn` does not include NULL operands. Combine it with `IsNull` when
NULL rows should match.

`Contains`, `StartsWith` and `EndsWith` escape pattern metacharacters for literal
search. `Like`/`ILike` accept SQL patterns. Values are bound, never interpolated.
Use `expr.Where` and `expr.OrderBy` to apply computed row predicates/orders; adapt
a model predicate with `expr.ModelPredicate` when composing expressions. Pass an
explicit sealed adapter when an expression API accepts a column, for example
`expr.Value(orm.TextExpr(Users.Email))`. Adapters preserve the column family,
model, value and role constraints.

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
handwritten declarations.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
