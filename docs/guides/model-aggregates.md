# Typed relation aggregates

<!-- compiled-examples -->

Aggregate descriptors from `database/orm/expr` work both in rows projections and
in per-parent relation slots. Related models need not be hydrated to compute a
summary. [Handwritten declarations](../../tests/fixtures/consumer/models/orm_relations.go)
own bindings, while ordinary model fields retain explicit loaded state.

## Declaring computed fields

Declare `relation.Value[V]` fields in the model and bind them with `orm.ValueOf` in
`orm.Define`. These slots are not persisted columns or write assignments. Supply
an exact-result aggregate with `slot.Using(expr.Related(relation, aggregate))`.
`expr.Related` validates the actual source and target owners as well as the Go
result type. A missing computation remains invalid when selected.

The [UserValues declaration](../../tests/fixtures/consumer/models/orm_relations.go)
includes counts, exact and floating summaries, extrema and pivot measures. Select
slots with `Users.Query().With(UserValues.OrderCount, UserValues.OrderTotal)`;
`orm.LoadFrom` refreshes slots and `orm.LoadMissingFrom` leaves loaded values intact.

## Loading and result types

| `expr` constructor | Result | Empty/all-NULL input |
| --- | --- | --- |
| `expr.Count[S]()` | `int64` | Zero; counts rows |
| `CountField(column)` | `int64` | Zero; counts non-NULL values |
| `CountDistinctField(column)` | `int64` | Zero; counts distinct non-NULL values |
| `Exists[S]()` | `bool` | False |
| `Sum` / `Avg` for exact numbers | `value.Nullable[decimal.Decimal]` | NULL |
| `SumFloat` / `AvgFloat` | `value.Nullable[float64]` | NULL |
| `SumInterval` / `AvgInterval` | `value.Nullable[temporal.Interval]` | NULL |
| `Min` / `Max` for ordered fields | Nullable present input type | NULL |

Exact summaries cast to numeric, including integer inputs; float summaries use
double precision. SQL precision/overflow and result codec bounds still apply.
Enums do not gain ordered/numeric capabilities from their underlying Go type.
Nullable inputs retain only one nullable result layer. Alias aggregates use `*At`;
derived rows operands use `*Row`/`*NullableRow` constructors.

`slot.Get()` returns `(V, loaded)`. Unrequested, loaded zero and loaded SQL NULL
are different states. Reading a slot never performs I/O. Parent Count/Exists do
not load slots. Collected parents are discarded if any load fails.

## Request-specific scopes

Relation filters restrict the input before grouping. Aggregate `Filter` changes
one measure, retaining the complete scoped input for cardinality checks. Derive
a slot with `Using` for a request-specific measure. A parent remains when no
related row matches: its count becomes zero. Authorization is still the caller's
responsibility; loading supplied parents does not re-fetch or authorize them.

Related targets and pivots retain independent soft-delete/global scopes.
Aggregate inputs cannot request eager-loaded children. Select ordinary relations
separately or select slots within a nested relation. Row ordering does not alter
these summaries.

## Ad-hoc values, ordering and filtering by relation counts

`expr.RelatedValue(relation, aggregate)` produces a correlated row expression.
Counts are required; sums retain SQL NULL for no input. Use it with `expr.Where`,
`expr.OrderBy` or `rows.MapSelected` after `expr.AsSelected`. `rows.WithValue` pairs a
complete model with the computed value while preserving its retrieval/loading
pipeline. The [office consumer](../../tests/fixtures/consumer/officequeries/office_postgres_test.go)
shows these forms without a permanent model slot.

## Many-to-many target and pivot summaries

Pass a relation to `expr.Related` to measure targets, or `orm.Pivot(relation)` to
measure pivots while keeping both sides' filters. Count includes joined edges;
`CountDistinctField` on the target key counts different targets. NULL keys,
missing targets and filtered targets do not join. Singular relations enforce
cardinality, and ambiguous pivot joins fail rather than silently multiplying a
measure.

## Query and resource behavior

Each selected slot groups parent keys in bounded batches. Slots run separate
queries, not one query per parent; empty batches perform no SQL. Limits bound
returned groups and attached values, including empty results. They do not bound
server scan/sort work. SQL key equality must agree with the framework's key
matching; custom collations or codecs that collapse different keys can cause
ambiguous or unmatched group errors. Rows close before the next branch executes. Use suitable
transaction isolation when separate aggregate statements need one snapshot.

[Projections](model-projections.md) reuse these expressions for grouped reports;
[aggregate FILTER](model-aggregate-filters.md) and [windows](model-windows.md) retain
the same result codecs and phase rules.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
