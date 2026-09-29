# Typed relation aggregates

Relation aggregates compute summaries without hydrating the related models. Handwritten model fields declare the result type and explicit loaded state; generation owns their attachment. The [consumer aggregate declarations](../../tests/fixtures/consumer/models/aggregates.go) and [PostgreSQL acceptance](../../tests/fixtures/consumer/model_aggregates_postgres_test.go) compile and execute the examples below.

## Declaring computed fields

Add non-persisted `relation.Value[V]` fields to a model:

```go
OrderCount   relation.Value[int64]
OrderTotal   relation.Value[value.Nullable[decimal.Decimal]]
OrderAverage relation.Value[value.Nullable[decimal.Decimal]]
```

These slots are excluded from SQL columns, write drafts and model row decoders. They cannot carry persistence column/default tags. Models with slots implement `DefineAggregates() UserAggregateSet`; generation creates that concrete set and `UserAggregates()`.

```go
func (User) DefineAggregates() UserAggregateSet {
    return UserAggregateSet{
        OrderCount: query.Related(UserRelations().Orders, query.Count[Order]()),
        OrderTotal: query.Related(UserRelations().Orders, OrderFields().TotalCents.Sum()),
        OrderAverage: query.Related(UserRelations().Orders, OrderFields().TotalCents.Avg()),
        // Supply the other declared slots in this model as well.
    }
}
```

`Related` checks that its aggregate input belongs to the relationship's target model. Its result must match the generated slot exactly. Wrong owners, integer/decimal result mismatches and lost nullability fail compilation. Missing computations remain invalid zero descriptors and fail when selected. Keep `DefineAggregates` and `DefineRelations` pure declarations; do not perform I/O or call the aggregate constructor recursively from its own definition.

## Loading and result types

```go
users, err := models.QueryUsers().With(
    models.UserAggregates().OrderCount,
    models.UserAggregates().OrderTotal,
).All(ctx, db)
```

Use the same model-owned `With` as ordinary relations. `Find`, `First`, pagination, `Load` and `LoadMissing` preserve these descriptors. Nested ordinary relations can select aggregates on their target or pivot models. `Count` and `Exists` on the parent query do not load aggregate slots; writes reject loading clauses. `Each` processes eager clauses in [bounded batches](model-chunks.md).

`slot.Get()` returns `(V, bool)`, with the boolean reporting loaded state. `IsLoaded()` does the same check. Thus an unrequested count differs from a computed zero, and an unrequested total differs from a computed SQL NULL. Read the inner `value.Nullable` to distinguish SQL NULL from a numeric zero. Accessing a slot performs no I/O.

| Operation | Result type | Empty/all-NULL behavior |
| --- | --- | --- |
| `query.Count[Model]()` | `int64` | Counts rows; zero when absent |
| `field.Count()` | `int64` | Counts non-NULL field values |
| `field.CountDistinct()` | `int64` | Counts distinct non-NULL field values |
| `query.Exists[Model]()` | `bool` | False for an absent group |
| Integer/decimal `Sum()` and `Avg()` | `value.Nullable[decimal.Decimal]` | SQL NULL |
| Float `Sum()` and `Avg()` | `value.Nullable[float64]` | SQL NULL |
| Ordered-field `Min()` and `Max()` | `value.Nullable[FieldType]` | SQL NULL |

Numeric operations appear only on generated numeric fields. Text, temporal and enum fields cannot request sums/averages. Ordered fields retain their concrete type through extrema, including named text types and temporal values. Enum and model-ID scalar fields support counts but do not gain numeric operations.

Foundry casts integer/decimal inputs to PostgreSQL `numeric` before SUM/AVG, avoiding intermediate integer overflow and fractional truncation. Finite division precision still belongs to PostgreSQL; an average is not an arbitrary-precision rational number. Float inputs are promoted to double precision and retain approximate semantics. These choices build on PostgreSQL's [aggregate return types and null behavior](https://www.postgresql.org/docs/18/functions-aggregate.html). Result codecs reject non-finite floats and decimal results outside Foundry's declared digit bound.

## Request-specific scopes

[Aggregate `Filter`](model-aggregate-filters.md) applies typed row predicates to the measure itself, including target and pivot computations. It retains the relation's complete scoped input for cardinality checks. Query/relation `Where` instead restricts that input before aggregation.

Declare parent and related filters separately. Derive a generated slot with `Using` to change the computation while retaining the source and result type:

```go
count := models.UserAggregates().OrderCount.Using(query.Related(
    models.UserRelations().Orders.Where(models.OrderFields().TotalCents.Gt(1)),
    query.Count[models.Order](),
))
users, err := models.QueryUsers().With(count).All(ctx, db)
```

The parent remains in the result when no related row matches; its count becomes zero. `Load` does not authorize or re-fetch supplied parents. Authorization and soft-deletion scopes must be explicit until their owning milestones integrate them.

Aggregate inputs use the relation's filters. Row ordering does not change these summaries and is omitted from grouped SQL. A relation passed to `Related` cannot request eager-loaded children: those clauses would serve no role in the aggregate and are rejected. Select ordinary loaded relationships separately, or select an aggregate inside a relationship when the nested model itself needs a computed field.

```go
orders, err := models.QueryOrders().With(
    models.OrderRelations().Buyer.With(models.UserAggregates().OrderCount),
).All(ctx, db)
```

## Ad-hoc values, ordering and filtering by relation counts

Without a model slot, `query.RelatedValue` computes the same typed aggregate as a correlated scalar subquery for each source row:

```go
headcount := query.RelatedValue(OfficeRelations().Colleagues, query.Count[Employee]())
payroll := query.RelatedValue(OfficeRelations().Colleagues, EmployeeFields().Salary.Sum())

busiest := QueryOffices().OrderBy(headcount.Desc())            // order by relation count
large := QueryOffices().Where(query.OrderRow(headcount).Gte(10)) // filter by relation count
pairs, err := query.WithValue(busiest.Query, payroll).All(ctx, db) // []query.Annotated[Office, value.Nullable[decimal.Decimal]]
```

The result is a `RowExpression` of the computation's own type: `COUNT` is never NULL, while `SUM` of no rows is NULL. It uses the relationship's keys, filters and scopes (many-to-many and through relationships include their joins), and composes wherever a row value does: ordering, predicates, projections via `Value()`, and `query.WithValue`, which reads complete models paired with the value in one statement (`Annotated{Model, Value}`), running retrieval hooks and loading the query's eager relations. The [office consumer](../../tests/fixtures/consumer/officequeries/office_postgres_test.go) exercises these forms.

## Many-to-many target and pivot summaries

The relationship itself measures targets. `Pivot()` selects the concrete pivot input while retaining both target and pivot filters:

```go
GroupCount: query.Related(UserRelations().Groups, query.Count[Group]()),
GroupDistinct: query.Related(UserRelations().Groups, GroupFields().Code.CountDistinct()),
GroupPriority: query.Related(UserRelations().Groups.Pivot(), MembershipFields().Priority.Sum()),
```

A row count includes distinct pivot edges, even when several point to the same target. Use `CountDistinct` on the target key for the number of different targets. Null pivot keys, missing targets and filtered-out targets do not produce joined rows. Singular aggregates enforce at most one target; many-to-many aggregates compare joined row counts with distinct pivot primary keys and reject ambiguous joins with `database.TooManyRows`.

## Query and resource behavior

Each aggregate groups distinct non-null parent keys in configured batches, using the shared SELECT AST and PostgreSQL compiler. Selected aggregates execute separately; no query runs per individual parent. Multiple aggregate slots are not currently combined into one SELECT. Empty key batches perform no SQL. `LoadMissing` skips already-computed slots, including zero and SQL NULL; use `Load` to refresh them.

`RelationLimits.MaxRows` caps returned group rows and, separately, attached model/scalar values across all selected branches. Each computed slot counts once per parent, including an empty result. This bounds materialized results; it does not cap the number of rows PostgreSQL examines while aggregating. Database cancellation/timeouts and appropriate indexes govern that work. Related SQL rows are closed before another branch runs, so a caller-owned transaction can remain the executor.

Grouped keys are decoded with the source model's codec and compared through canonical database representations, including equal floating-point signed zeros. Database equality must agree with declared key semantics; custom collations require canonical keys as described in the [relation guide](model-relations.md). Each aggregate is a separate statement: use a transaction with suitable isolation for a common snapshot.

Invalid bindings, duplicate slots, cardinality errors, cancellation, malformed results and resource overflow discard the entire collected parent result. Computation callbacks are generated ordinary Go, with the same value-copy boundaries as relation loading. No result uses string-based field lookup or an untyped application map.

[Declared projections](model-projections.md) also execute these typed expressions as scalar summaries and grouped result records, including [joined model sources](model-joins.md), without attaching model slots. Their [HAVING and ordering APIs](model-projections.md#filter-and-order-groups) filter computed groups and rank aggregate values. Counts return `OrderedAggregate[Scope, int64]`; sums, averages and extrema return `NullableOrderedAggregate[Scope, Value]`, preserving nullable `Value()` results while comparisons accept concrete values. `Related` accepts their shared `AggregateExpression[Model, Result]` contract. Boolean existence retains scalar equality/membership capabilities. Relation loading does not accept HAVING as a target row scope; use ordinary `Where` to restrict aggregate inputs or `Filter` for an individual measure. See [relationship predicates](model-relationship-filters.md), [CTEs](model-ctes.md) and [windows](model-windows.md) for composition with other delivered advanced queries.
