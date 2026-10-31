# Typed subqueries and derived records

<!-- compiled-examples -->

`database/orm/rows` composes complete models, handwritten projections and scalar
selections. Each SELECT owns its scope; parameters use statement-wide numbering.
IDs, enum codecs and nullable value types survive each boundary.

## Use a report as a query source

Wrap a projection in `rows.RecordOf`, then alias it with `rows.AsRecord[Tag]`.
`rows.At(rows.Scope(alias), schema.Field)` addresses its declared output fields.
Inner grouping, HAVING, DISTINCT, ordering and limit/offset remain inside the
derived SELECT. The outer WHERE sees only its output, not hidden input columns.
The [derived report example](../../tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go)
shows this with buyer totals.

`rows.ModelRecord` adapts a complete model; `rows.As[Tag]` is the direct model
alias helper. Missing mappings, eager-loading clauses and invalid alias metadata
fail before execution. Reusing a SQL alias name in an independent SELECT is valid;
conflicting names/identities in one scope are not.

## Join derived records

Join aliases using `rows.InnerJoin`/`LeftJoin` and typed `rows.On` or computed
conditions. A preserved side uses `LeftScope`/`RightScope`; a missing side uses
`NullableLeftScope`/`NullableRightScope`. `rows.AtNullable` adds a nullable layer to
a required output field; `rows.AtNullableValue` keeps an already-nullable field
singly nullable. The corresponding model-column helpers live in `expr`.

A joined projection may itself become a complete record source. This establishes
a new declared boundary without exposing its internal aliases. See [joins](model-joins.md).

## Select one typed value

`rows.SelectScalar(source, selected)` creates `rows.Scalar[S,V]`. It remains a
subquery value source after `OrderScalar`, `LimitScalar`, `OffsetScalar`,
`WhereScalar`, `GroupByScalar`, `HavingScalar` or scalar DISTINCT adapters.
`rows.ReadScalar` supplies execution; `CompileRead`, `PreflightRead` and `EachRead`
operate on that read handle without flattening the inner SELECT.

`rows.InQuery(left, inner)` requires an exact value type; `InNullableQuery`
explicitly accepts a nullable inner result. Model-owned IDs cannot be substituted.
SQL NOT IN with a NULL inner value can remain unknown for non-matches; it is not
an automatic complement. Filter NULLs explicitly if the set should exclude them.

## Existence and scalar values

`rows.ExistsQuery(outer, inner)` anchors an uncorrelated existence predicate to the
outer scope. It asks whether the inner selected window has a row. An ungrouped
COUNT over empty input still has one row unless HAVING or a window removes it.

`expr.AsSelected(rows.ScalarRowQuery(outer, inner))` returns a selected nullable value. An empty inner result
becomes NULL; multiple rows produce PostgreSQL's scalar cardinality error. No
implicit limit is added. `expr.AsSelected(rows.ScalarNullableRowQuery(outer, inner))` keeps one nullable layer.
`ScalarRowQuery`/`ScalarNullableRowQuery` create row operands for outer comparisons;
the inner SELECT retains its own aggregate/window phase.

## Failure and resource behavior

Compiler validation covers scope, complete mappings, codecs, grouping, depth and
parameter bounds. A bad declaration returns no statement; preflight performs no
I/O. Collection failures publish no partial slice; streaming closes on all exits.
Use a savepoint if a PostgreSQL cardinality error must not abort the caller's
remaining transaction work.

Use [explicit correlation](model-correlations.md) to capture outer fields,
[CTEs](model-ctes.md) to share a definition and [read terminals](model-projections.md#execution-and-failures)
for bounded execution. Ordinary sources cannot silently acquire outer references
or erase transaction requirements.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
