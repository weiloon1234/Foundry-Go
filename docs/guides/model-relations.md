# Typed model relations

Relations retain source model, target model and key types. The [handwritten model fields](../../tests/fixtures/consumer/models/models.go) declare `relation.One[User]`, `relation.Many[Order]` or `relation.Through[Group, Membership]`. These fields are relation state, not persisted columns: generation excludes them from SQL, drafts and codecs.

## Declaring keys in Go

Models with relation fields implement `DefineRelations`, returning their generated relation-set type. The [consumer declarations](../../tests/fixtures/consumer/models/relations.go) compile this pattern:

```go
func (User) DefineRelations() UserRelationSet {
    return UserRelationSet{
        Introducer: query.BelongsTo(UserFields().IntroducerID, UserFields().ID),
        Referrals: query.HasMany(UserFields().ID, UserFields().IntroducerID),
        Orders: query.HasMany(UserFields().ID, OrderFields().BuyerID),
        SingleOrder: query.HasOne(UserFields().ID, OrderFields().BuyerID),
        Groups: query.ManyToMany(UserFields().ID, MembershipFields().UserID,
            MembershipFields().GroupCode, GroupFields().Code),
        Friends: query.ManyToMany(UserFields().ID, FriendshipFields().FromID,
            FriendshipFields().ToID, UserFields().ID),
    }
}
```

Use generated Go field descriptors; no SQL names or database types are repeated. Key types must match, including named natural keys and model-owned IDs. A nullable field retains its underlying key type for compatibility, with missing values handled at runtime. A relation set also checks the declared source/target and singular/collection cardinality. Missing descriptors remain invalid zero values and fail when selected for loading.

Generation emits `UserRelations()`, which binds the declarations to complete model metadata and typed field getters/setters. Each model also receives `FoundryQuery()` for generated integrations across packages. Keep `DefineRelations` a pure declaration of base relationships; request-specific scopes and finite nested loading belong at the call site. Self-relations work because `relation.One[T]` stores its model behind a private pointer. Imported targets work when their package has generated model metadata; ordinary Go import-cycle rules still apply.

The generated `Bind` calls are an explicit metadata boundary, not normal consumer configuration. They require bare source/target/pivot metadata queries and reject query options rather than silently dropping predicates. Declare filters/order/nesting on the relation descriptor itself.

## Loading and reading values

```go
users, err := models.QueryUsers().
    With(models.UserRelations().Introducer,
        models.UserRelations().Orders.OrderBy(models.OrderFields().TotalCents.Desc())).
    All(ctx, db)
```

`With` preserves the generated query wrapper, so typed `Find`, `RequireFind`, `First`, pagination and other collection reads remain available. Relations load after parent rows are closed, allowing the same executor to be a single-connection transaction. Count/Exists do not execute eager loads. Model writes reject eager-loading clauses.

For `One[T]`, `Get()` returns `(value.Optional[T], bool)`: the boolean reports loaded state, and the optional reports whether a related model exists. Thus not loaded, loaded empty and loaded present remain distinct. `Many[T].Get()` returns `([]T, bool)` with the same loaded flag, including for empty collections; the slice is a caller-owned copy. `Len()` and `All()` (an `iter.Seq2[int, T]` yielding each model by value) read a large collection without copying it, and `Through` offers the same pair for links. Both expose `IsLoaded()`. Ordinary Go field access and these getters perform no I/O.

Eager loading copies the caller's parent slice once per load, attaches each relation branch in place on that copy, and adopts freshly loaded groups without re-copying them. The whole relation tree is validated once before the parent read, not again for every nested level and key batch.

`query.HasOne` and `BelongsTo` require at most one matching target for each key. Multiple matching rows report `database.TooManyRows` and discard the entire result. Foundry never chooses an arbitrary first match. Enforce physical uniqueness where the domain requires it. `HasMany` retains the target query's order and appends its primary key as a tie-breaker when absent.

## One of many

When a parent has many targets but a slot needs one of them, declare the choice explicitly, like Laravel's `latestOfMany`/`ofMany`:

```go
o, e := OfficeFields(), EmployeeFields()
return OfficeRelationSet{
    Newest:    query.HasOne(o.ID, e.OfficeID).LatestOfMany(),        // highest primary key
    Earliest:  query.HasOne(o.ID, e.OfficeID).OfMany(e.HiredAt.Asc()),
    TopEarner: query.HasOne(o.ID, e.OfficeID).OfMany(e.Salary.Desc()),
}
```

Each parent loads the first target in that order (the target primary key breaks ties; `OldestOfMany` uses the lowest key), in one `DISTINCT ON` query per key batch. The relationship's filters and scopes take part in the choice: `TopEarner.Where(e.Salary.Lt(25))` loads the best-paid employee under 25. `WhereHas` therefore matches when any filtered target exists. Relation aggregates over a one-of-many relationship are rejected; aggregate its `HasMany` form. The [office consumer](../../tests/fixtures/consumer/officequeries/office_postgres_test.go) exercises these relationships.

## Through an intermediate model

`HasManyThrough` and `HasOneThrough` reach targets through an intermediate model, like Laravel's `hasManyThrough`/`hasOneThrough`:

```go
r, o, e := RegionFields(), OfficeFields(), EmployeeFields()
Employees: query.HasManyThrough(r.ID, o.RegionID, o.ID, e.OfficeID) // Region -> Office -> Employee
Region:    query.HasOneThrough(e.OfficeID, o.ID, o.RegionID, r.ID)   // Employee -> Office -> Region
```

The arguments pair the source key with the intermediate's first key, then the intermediate's second key with the target key; the compiler checks both key types and all three models. The slots are ordinary `relation.Many`/`relation.One` fields. Targets load in one joined query per key batch, the intermediate's soft deletion and [global scopes](model-global-scopes.md) apply, and a target reachable through several intermediate rows appears once per path. `WhereHas`, relation aggregates and `query.RelatedValue` use the same join. The intermediate is any generated model (`query.ModelQuery`); it needs no relation slot of its own. A one-of-many choice over `HasOneThrough` is made per parent across all its intermediate rows: `HasOneThrough(r.ID, o.RegionID, o.ID, e.OfficeID).OfMany(e.HiredAt.Desc())` loads each region's latest hire from any of its offices.

## Polymorphic relationships

A model that takes part in polymorphic relationships declares its stored discriminator once; this method is the typed morph map, so call sites never pass type strings:

```go
func (Office) MorphName() query.MorphName { return "office" }
func (Region) MorphName() query.MorphName { return "region" }

//foundry:model table=office_notes
type Note struct {
    ID          model.ID[Note]
    SubjectType query.MorphName
    SubjectID   string
    Body        string
    Region      relation.One[Region]
    Office      relation.One[Office]
}
```

| Constructor | Relationship |
| --- | --- |
| `MorphMany(o.Code, n.SubjectID, n.SubjectType)` / `MorphOne` | targets storing the parent's key and morph name |
| `MorphTo(n.SubjectID, n.SubjectType, o.Code)` | the parent of one type; declare one slot per possible parent type |
| `MorphToMany(o.Code, p.LabelableID, p.LabelableType, p.LabelID, l.ID)` | many-to-many through a pivot storing the source's morph name |
| `MorphedByMany(l.ID, p.LabelID, p.LabelableType, p.LabelableID, o.Code)` | its inverse for one morphable target type |

Every morph target shares one key type (here a unique `Code`), because one stored id column references them all. Instead of an untyped "any parent" slot, `MorphTo` gives each possible parent type its own typed `relation.One` slot: loading a note fills the slot matching its stored morph name and loads the others empty, and `WhereHas(r.Office)` matches only notes of offices. `MorphToMany` pivot writes (`Attach`, `Sync`, ...) fill the morph name automatically. Aggregates over `MorphTo` are rejected. A `MorphName` outside the identifier grammar (letters, digits and underscores; for example a namespaced `App\Models\Post` or `blog-post`) makes every query, load and write through that relationship fail validation, including after generated binding. The [office consumer](../../tests/fixtures/consumer/officequeries/office_postgres_test.go) exercises all four forms.

## Scopes and nested loading

Apply related predicates explicitly with `Where`, and nested descriptors with `With`:

```go
relation := models.UserRelations().Introducer.
    Where(models.UserFields().Status.Eq(models.StatusActive)).
    With(models.UserRelations().Introducer)
users, err := models.QueryUsers().With(relation).All(ctx, db)
```

Predicates and nested descriptors must belong to the target model. A filtered-out target becomes loaded empty; it does not remove its parent. Parent scopes and related scopes are separate declarations. Automatic [soft-delete visibility](model-soft-deletes.md) applies independently to targets and pivots. Authentication remains a separate authorization concern.

To filter the parent itself, use [relationship existence filters](model-relationship-filters.md): `QueryUsers().WhereHas(UserRelations().Orders.Where(...))` or `WhereDoesntHave`. Descriptors also expose a parent-owned `Exists()` predicate for boolean composition and nested relationship conditions. These filters perform no eager loading; use `With` explicitly, optionally reusing the same descriptor.

No relation-global limit or offset is accepted because it would truncate a batch rather than each parent's collection. Repeating a relation slot in one query is invalid; derive one descriptor containing its complete scope. Descriptors and queries use value copies, and `With` captures pointer arguments by value before retaining them.

## Many-to-many and typed pivots

Declare a `relation.Through[Target, Pivot]` field. Both types are ordinary generated models; the [membership fixture](../../tests/fixtures/consumer/models/membership.go) uses a natural primary key for its pivot and a nullable named natural target key. `ManyToMany(sourceKey, pivotSourceKey, pivotTargetKey, targetKey)` checks both key pairs and the pivot owner at compilation. The same model can be the source and target.

```go
groups := models.UserRelations().Groups.
    Where(models.GroupFields().Name.Eq("same")).
    WherePivot(models.MembershipFields().Priority.Gt(0)).
    OrderByPivot(models.MembershipFields().Priority.Asc()).
    OrderBy(models.GroupFields().Code.Desc()).
    With(models.GroupRelations().Owner).
    WithPivot(models.MembershipRelations().Inviter)
users, err := models.QueryUsers().With(groups).All(ctx, db)
```

`user.Groups.Get()` returns `([]relation.Link[Group, Membership], bool)`. Each link exposes a concrete `Model` and `Pivot`. The loaded flag distinguishes not loaded from loaded empty; retrieving the collection copies its slice. Target models shared by different parents never carry another parent's pivot data. Distinct pivot rows for the same target remain distinct links. If one pivot row matches several target rows, loading fails with `database.TooManyRows`; enforce target-key uniqueness in the database when required.

`Where`/`OrderBy` accept target descriptors; `WherePivot`/`OrderByPivot` accept pivot descriptors. Ordering preserves the combined method-call order and appends missing target/pivot primary keys as tie-breakers. `With` loads target relations, and `WithPivot` loads pivot relations. Both share the root loading limits. Duplicate ordering columns and repeated relation slots fail validation.

Each source-key batch executes one inner join through the shared SELECT AST/compiler with distinct target/pivot aliases. PostgreSQL applies filtering and ordering; Foundry does not reorder strings using Go's collation rules. Null pivot keys, missing targets and filtered-out targets do not create links. Refer to PostgreSQL's [join and alias semantics](https://www.postgresql.org/docs/18/queries-table-expressions.html).

Complete target and pivot rows reuse their generated codecs and decoders. A decoding failure discards the entire collected result. Pivot fields are neither raw maps nor partial models. Joined decoding requires owned values, excluding `sql.RawBytes`; see [database/sql scanning](https://pkg.go.dev/database/sql#Rows.Scan). [Typed joins](model-joins.md) can explicitly join model/pivot sources into separate declared projections; they do not change this relation's complete target/pivot loading behavior. [Typed attach/detach helpers](model-relation-writes.md) reuse these declarations and the pivot model's ordinary lifecycle.

## Explicit batch and lazy loading

`query.With(...).Load(ctx, executor, parents)` returns copies of existing parent models with the requested relations. Passing a one-element slice is explicit lazy loading; larger slices use the same batching. `LoadMissing` skips slots already loaded, including loaded-empty slots. To refresh a loaded relation, use `Load`.

These operations do not re-fetch or filter supplied parent models. The caller owns their provenance and authorization; query predicates on the parent are not an authorization check for an existing slice. Related scopes are still applied to the target queries. An error returns no result and leaves input parent structs unchanged. Relation containers copy their top-level values/slices; arbitrary pointer/map/slice fields inside domain models retain normal Go value-copy semantics.

`Each` with eager clauses uses `query.DefaultChunkSize` parent batches. Parent rows close before child queries, and the complete batch loads before any callback. Use `EachChunked` or `Chunk` for an explicit size, or `EachByID`/`ChunkByID` for primary-key traversal. Relation limits reset per batch; callbacks can run sequential work on the same transaction. See [bounded model iteration](model-chunks.md) for ordering, mutation, consistency and failure semantics.

## Batching, consistency and limits

Distinct non-null keys are queried in bounded `IN` batches through the shared query AST/compiler. Empty key sets issue no child SQL and attach loaded-empty values. A belongs-to relation with many parents sharing one key fetches that target once per loading branch. Nested branches batch their complete target collection rather than loading each parent individually.

`query.DefaultRelationLimits()` supplies batch size, maximum related values and nesting depth. Override with `WithRelationLimits`. `MaxRows` separately caps fetched SQL rows and attached model/scalar values across all branches, so duplicate input parents cannot multiply a collection beyond the same budget. A joined target/pivot row counts as one fetched row and two attached model values; each computed aggregate slot counts once per parent. Nested attachments also count. An over-budget read uses at most the remaining row allowance plus one lookahead row, then fails without publishing partial parents. Invalid limits/graphs fail before parent SQL. Parent collection size is still controlled by the caller's query limit or supplied slice.

Key grouping uses the declared comparable Go key after codec normalization. Database equality for relationship keys must agree with that representation. Use canonical natural keys; case-insensitive or other nondeterministic equality semantics require an explicit codec/schema design. A returned key that does not match requested keys fails instead of being silently dropped.

Eager loading uses separate SQL statements. Use a caller-owned transaction with appropriate isolation when parent/child snapshot consistency matters. The framework does not promise a common snapshot across pool reads or separate requests. SQL/codec/row-cleanup failures and context cancellation discard the complete collected result; caller-owned streaming side effects remain outside this guarantee.

## Current delivery and verification

Milestone 06 implements belongs-to, has-one, has-many and many-to-many with typed pivots, nullable/self/natural keys, typed scopes, nested eager loading, explicit Load/LoadMissing and bounded model iteration. [Typed relation aggregates](model-aggregates.md) use generated computed slots without loading related models. [Declared projections](model-projections.md) return independent result types for partial reads, grouped reports and scalar summaries. [Typed joins](model-joins.md) include inner/outer/self joins with explicit scoped fields. The [milestone completion review](../../blueprint/06-relations-and-advanced-queries.md#completion-review) connects these APIs and advanced queries to their consumer coverage; the master roadmap owns verification status.

The [PostgreSQL acceptance fixture](../../tests/fixtures/consumer/model_relations_postgres_test.go) verifies query counts, key batching, cardinality errors, nullable inverses, empty collections, nested loading, explicit scopes, natural keys, loaded-state preservation and resource bounds. Negative compiler fixtures reject incompatible keys, owners, targets and scope predicates. Generator tests verify fresh relation generation, determinism and no publication on invalid declarations.

The [many-to-many PostgreSQL fixture](../../tests/fixtures/consumer/model_through_postgres_test.go) covers typed nullable/natural pivots, duplicate edges, self-relations, target and pivot scopes, SQL ordering, nested loading on both models, skipped missing targets, ambiguous joins, failed pivot hydration and separate fetch/attachment budgets. Compile-failure fixtures reject mismatched keys, mixed pivot owners, wrong pivot filters/orderings and incompatible link payloads.
