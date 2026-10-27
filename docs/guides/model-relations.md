# Typed model relations

Relations retain source model, target model and key types. The [handwritten model fields](../../tests/fixtures/consumer/models/models.go) declare `relation.One[User]`, `relation.Many[Order]` or `relation.Through[Group, Membership]`. These fields are relation state, not persisted columns: bind them with typed slot declarations rather than persisted columns. [Model extension slots](model-extension-slots.md) for translated text, attachments and metadata load through the same `With` and `LoadFrom` paths.

## Declaring keys in Go

Bind model slots and keys in handwritten `orm.Define` declarations, then compose
owner-typed relationships at package scope. The [consumer declarations](../../tests/fixtures/consumer/models/orm_relations.go) demonstrate this pattern:

```go
introducer := orm.BelongsToOf(models.Users.Introducer, models.Users.IntroducerID, models.Users.ID)
orders := orm.HasManyOf(models.Users.Orders, models.Users.ID, models.Orders.BuyerID)
```

Use the bound slot and column handles; no SQL names or codecs are repeated. Key
types must match, including natural keys and model-owned IDs. Nullable columns
retain their underlying key compatibility. The slot checks source, target and
cardinality. Zero descriptors fail before SQL. `orm.Check` validates declarations
at startup. Runtime ownership checks also reject columns from another declaration
of the same Go model. Request-specific scopes and finite nested loading belong
at the call site. Self-relations use the private pointer in `relation.One[T]`;
ordinary Go import-cycle rules still apply.

## Loading and reading values

```go
users, err := models.Users.Query().With(models.UserLinks.Introducer,
    models.UserLinks.Orders.OrderBy(models.Orders.TotalCents.Desc())).All(ctx, db)
```

`With` preserves the model query and its filters, ordering and terminals. Relations load after parent rows are closed, allowing the same executor to be a single-connection transaction. Count/Exists do not execute eager loads. Model writes reject eager-loading clauses.

For `One[T]`, `Get()` returns `(value.Optional[T], bool)`: the boolean reports loaded state, and the optional reports whether a related model exists. Thus not loaded, loaded empty and loaded present remain distinct. `Many[T].Get()` returns `([]T, bool)` with the same loaded flag, including for empty collections; the slice is a caller-owned copy. `Len()` and `All()` (an `iter.Seq2[int, T]` yielding each model by value) read a large collection without copying it, and `relation.Through` offers the same pair for links. Both expose `IsLoaded()`. Ordinary Go field access and these getters perform no I/O.

Eager loading copies the caller's parent slice once per load, attaches each relation branch in place on that copy, and adopts freshly loaded groups without re-copying them. The whole relation tree is validated once before the parent read, not again for every nested level and key batch.

`orm.HasOneOf` and `BelongsToOf` require at most one matching target for each key. Multiple matching rows report `database.TooManyRows` and discard the entire result. Foundry never chooses an arbitrary first match. Enforce physical uniqueness where the domain requires it. `HasManyOf` retains the target query's order and appends its primary key as a tie-breaker when absent.

## One of many

When a parent has many targets but a slot needs one of them, declare the choice explicitly, like Laravel's `latestOfMany`/`ofMany`:

```go
newest := orm.HasOneOf(Offices.Newest, Offices.ID, Employees.OfficeID).LatestOfMany()
earliest := orm.HasOneOf(Offices.Earliest, Offices.ID, Employees.OfficeID).OfMany(Employees.HiredAt.Asc())
```

Each parent loads the first target in that order (the target primary key breaks ties; `OldestOfMany` uses the lowest key), in one `DISTINCT ON` query per key batch. The relationship's filters and scopes take part in the choice: `TopEarner.Where(e.Salary.Lt(25))` loads the best-paid employee under 25. `orm.Has` therefore matches when any filtered target exists. Relation aggregates over a one-of-many relationship are rejected; aggregate its `HasMany` form. The [office consumer](../../tests/fixtures/consumer/officequeries/office_postgres_test.go) exercises these relationships.

## Through an intermediate model

`orm.HasManyThroughOf` and `orm.HasOneThroughOf` reach targets through an intermediate model, like Laravel's `hasManyThrough`/`hasOneThrough`:

```go
employees := orm.HasManyThroughOf(Regions.Employees, Regions.ID,
    Offices.RegionID, Offices.ID, Employees.OfficeID)
region := orm.HasOneThroughOf(Employees.Region, Employees.OfficeID,
    Offices.ID, Offices.RegionID, Regions.ID)
```

The arguments pair the source key with the intermediate's first key, then the intermediate's second key with the target key; the compiler checks both key types and all three models. The slots are ordinary `relation.Many`/`relation.One` fields. Targets load in one joined query per key batch, the intermediate's soft deletion and [global scopes](model-global-scopes.md) apply, and a target reachable through several intermediate rows appears once per path. `orm.Has`, relation aggregates and `rows.RelatedValue` use the same join. The intermediate is any declared model; it needs no relation slot of its own. A one-of-many choice over `HasOneThroughOf` is made per parent across all its intermediate rows: `orm.HasOneThroughOf(r.LatestHire, r.ID, o.RegionID, o.ID, e.OfficeID).OfMany(e.HiredAt.Desc())` loads each region's latest hire from any of its offices.

## Polymorphic relationships

A model that takes part in polymorphic relationships declares its stored discriminator once; this method is the typed morph map, so call sites never pass type strings:

```go
func (Office) MorphName() model.MorphName { return "office" }
func (Region) MorphName() model.MorphName { return "region" }

type Note struct {
    ID          model.ID[Note]
    SubjectType model.MorphName
    SubjectID   string
    Body        string
    Region      relation.One[Region]
    Office      relation.One[Office]
}
```

| Constructor | Relationship |
| --- | --- |
| `orm.MorphManyOf(slot, o.Code, n.SubjectID, n.SubjectType)` / `orm.MorphOneOf` | targets storing the parent's key and morph name |
| `orm.MorphToOf(slot, n.SubjectID, n.SubjectType, o.Code)` | the parent of one type; declare one slot per possible parent type |
| `orm.MorphToManyOf(slot, o.Code, p.LabelableID, p.LabelableType, p.LabelID, l.ID)` | many-to-many through a pivot storing the source's morph name |
| `orm.MorphedByManyOf(slot, l.ID, p.LabelID, p.LabelableType, p.LabelableID, o.Code)` | its inverse for one morphable target type |

Every morph target shares one key type (here a unique `Code`), because one stored id column references them all. Instead of an untyped "any parent" slot, `MorphTo` gives each possible parent type its own typed `relation.One` slot: loading a note fills the slot matching its stored morph name and loads the others empty, and `WhereHas(r.Office)` matches only notes of offices. `MorphToMany` pivot writes (`Attach`, `Sync`, ...) fill the morph name automatically. Aggregates over `MorphTo` are rejected. A `model.MorphName` outside the identifier grammar (letters, digits and underscores; for example a namespaced `App\Models\Post` or `blog-post`) makes every query, load and write through that relationship fail validation, including after descriptor composition. The [office consumer](../../tests/fixtures/consumer/officequeries/office_postgres_test.go) exercises all four forms.

## Scopes and nested loading

Apply related predicates explicitly with `Where`, and nested descriptors with `With`:

```go
introducer := models.UserLinks.Introducer.
    Where(models.Users.Status.Eq(models.StatusActive)).With(models.UserLinks.Introducer)
users, err := models.Users.Query().With(introducer).All(ctx, db)
```

Predicates and nested descriptors must belong to the target model. A filtered-out target becomes loaded empty; it does not remove its parent. Parent scopes and related scopes are separate declarations. Automatic [soft-delete visibility](model-soft-deletes.md) applies independently to targets and pivots. Authentication remains a separate authorization concern.

To filter the parent itself, use [relationship existence filters](model-relationship-filters.md): `Users.Query().Where(orm.Has(UserLinks.Orders.Where(...)))` or `orm.Not(orm.Has(...))`. `orm.Has` produces a parent-owned predicate for boolean composition and nested relationship conditions. These filters perform no eager loading; use `With` explicitly, optionally reusing the same descriptor.

No relation-global limit or offset is accepted because it would truncate a batch rather than each parent's collection. Repeating a relation slot in one query is invalid; derive one descriptor containing its complete scope. Descriptors and queries use value copies, and `With` captures pointer arguments by value before retaining them.

## Many-to-many and typed pivots

Declare a `relation.Through[Target, Pivot]` field. Both types are ordinary declared models; the [membership fixture](../../tests/fixtures/consumer/models/membership.go) uses a natural primary key for its pivot and a nullable named natural target key. `orm.ManyToManyOf(slot, sourceKey, pivotSourceKey, pivotTargetKey, targetKey)` checks both key pairs and the pivot owner at compilation. The same model can be the source and target.

```go
groups := models.UserLinks.Groups.
    Where(models.Groups.Name.Eq("same")).
    WherePivot(models.Memberships.Priority.Gt(0)).
    OrderByPivot(models.Memberships.Priority.Asc()).
    OrderBy(models.Groups.Code.Desc()).
    With(models.GroupLinks.Owner).WithPivot(models.MembershipLinks.Inviter)
users, err := models.Users.Query().With(groups).All(ctx, db)
```

`user.Groups.Get()` returns `([]relation.Link[Group, Membership], bool)`. Each link exposes a concrete `relation.Link.Model` and `Pivot`. The loaded flag distinguishes not loaded from loaded empty; retrieving the collection copies its slice. Target models shared by different parents never carry another parent's pivot data. Distinct pivot rows for the same target remain distinct links. If one pivot row matches several target rows, loading fails with `database.TooManyRows`; enforce target-key uniqueness in the database when required.

`Where`/`OrderBy` accept target descriptors; `WherePivot`/`OrderByPivot` accept pivot descriptors. Ordering preserves the combined method-call order and appends missing target/pivot primary keys as tie-breakers. `With` loads target relations, and `WithPivot` loads pivot relations. Both share the root loading limits. Duplicate ordering columns and repeated relation slots fail validation.

Each source-key batch executes one inner join through the shared SELECT AST/compiler with distinct target/pivot aliases. PostgreSQL applies filtering and ordering; Foundry does not reorder strings using Go's collation rules. Null pivot keys, missing targets and filtered-out targets do not create links. Refer to PostgreSQL's [join and alias semantics](https://www.postgresql.org/docs/18/queries-table-expressions.html).

Complete target and pivot rows reuse their declaration codecs and complete row decoders. A decoding failure discards the entire collected result. Pivot fields are neither raw maps nor partial models. Joined decoding requires owned values, excluding `sql.RawBytes`; see [database/sql scanning](https://pkg.go.dev/database/sql#Rows.Scan). [Typed joins](model-joins.md) can explicitly join model/pivot sources into separate declared projections; they do not change this relation's complete target/pivot loading behavior. [Typed attach/detach helpers](model-relation-writes.md) reuse these declarations and the pivot model's ordinary lifecycle.

## Explicit batch and lazy loading

`orm.LoadFrom(ctx, executor, table.Query().With(relations...), parents)` returns copies of existing parent models with the requested relations. Passing a one-element slice is explicit lazy loading; larger slices use the same batching. `LoadMissingFrom` skips slots already loaded, including loaded-empty slots. To refresh a loaded relation, use `LoadFrom`.

These operations do not re-fetch or filter supplied parent models. The caller owns their provenance and authorization; query predicates on the parent are not an authorization check for an existing slice. Related scopes are still applied to the target queries. An error returns no result and leaves input parent structs unchanged. Relation containers copy their top-level values/slices; arbitrary pointer/map/slice fields inside domain models retain normal Go value-copy semantics.

`Query.Each` with eager clauses uses `orm.DefaultChunkSize` parent batches. Parent rows close before child queries, and the complete batch loads before any callback. Use `EachChunked` or `Chunk` for an explicit size, or `orm.EachByKey`/`orm.ChunkByKey` for primary-key traversal. Relation limits reset per batch; callbacks can run sequential work on the same transaction. See [bounded model iteration](model-chunks.md) for ordering, mutation, consistency and failure semantics.

## Batching, consistency and limits

Distinct non-null keys are queried in bounded `IN` batches through the shared query AST/compiler. Empty key sets issue no child SQL and attach loaded-empty values. A belongs-to relation with many parents sharing one key fetches that target once per loading branch. Nested branches batch their complete target collection rather than loading each parent individually.

`orm.DefaultRelationLimits()` supplies batch size, maximum related values and nesting depth. Override with `orm.WithRelationLimits`. `MaxRows` separately caps fetched SQL rows and attached model/scalar values across all branches, so duplicate input parents cannot multiply a collection beyond the same budget. A joined target/pivot row counts as one fetched row and two attached model values; each computed aggregate slot counts once per parent. Nested attachments also count. An over-budget read uses at most the remaining row allowance plus one lookahead row, then fails without publishing partial parents. Invalid limits/graphs fail before parent SQL. Parent collection size is still controlled by the caller's query limit or supplied slice.

Key grouping uses the declared comparable Go key after codec normalization. Database equality for relationship keys must agree with that representation. Use canonical natural keys; case-insensitive or other nondeterministic equality semantics require an explicit codec/schema design. A returned key that does not match requested keys fails instead of being silently dropped.

Eager loading uses separate SQL statements. Use a caller-owned transaction with appropriate isolation when parent/child snapshot consistency matters. The framework does not promise a common snapshot across pool reads or separate requests. SQL/codec/row-cleanup failures and context cancellation discard the complete collected result; caller-owned streaming side effects remain outside this guarantee.

## Current delivery and verification

Milestone 06 implements belongs-to, has-one, has-many and many-to-many with typed pivots, nullable/self/natural keys, typed scopes, nested eager loading, explicit LoadFrom/LoadMissingFrom and bounded model iteration. [Typed relation aggregates](model-aggregates.md) use declared computed slots without loading related models. [Declared projections](model-projections.md) return independent result types for partial reads, grouped reports and scalar summaries. [Typed joins](model-joins.md) include inner/outer/self joins with explicit scoped fields. The [milestone completion review](../../blueprint/06-relations-and-advanced-queries.md#completion-review) connects these APIs and advanced queries to their consumer coverage; the master roadmap owns verification status.

The [PostgreSQL acceptance fixture](../../tests/fixtures/consumer/model_relations_postgres_test.go) verifies query counts, key batching, cardinality errors, nullable inverses, empty collections, nested loading, explicit scopes, natural keys, loaded-state preservation and resource bounds. Negative compiler fixtures reject incompatible keys, owners, targets and scope predicates. Native declaration tests reject invalid slots, keys and ownership before SQL.

The [many-to-many PostgreSQL fixture](../../tests/fixtures/consumer/model_through_postgres_test.go) covers typed nullable/natural pivots, duplicate edges, self-relations, target and pivot scopes, SQL ordering, nested loading on both models, skipped missing targets, ambiguous joins, failed pivot hydration and separate fetch/attachment budgets. Compile-failure fixtures reject mismatched keys, mixed pivot owners, wrong pivot filters/orderings and incompatible link payloads.
