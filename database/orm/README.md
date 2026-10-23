# Declare and query a table

This API is in progress on the `goway-data-layer` branch. It currently covers
typed declarations, common reads and writes, numeric adjustments, direct eager
relations, inner joins and row projections. The existing `database/query` API
remains available and unchanged; later stages complete the new API.

Keep the model and its table declaration together. The table's second type
parameter is the primary key type, including named natural keys.

```go
type Product struct {
	Code  string
	Price int64
}

type ProductTable struct {
	orm.Table[Product, string]
	Code  orm.Text[Product, string]
	Price orm.Number[Product, int64]
}

var Products = orm.Define("products", func(b orm.Builder[Product], p *Product) ProductTable {
	table := orm.TableOf(b, &p.Code)
	return ProductTable{
		Table: table,
		Code:  orm.TextKey(table.Key()),
		Price: orm.NumberOf(b, &p.Price),
	}
})
```

Call `orm.Check(Products)` during startup to report declaration errors together.
Each query or write also rejects an invalid table.

Use the column handles to read and write models:

```go
products, err := Products.Query().
	Where(Products.Price.Between(100, 500)).
	OrderBy(Products.Code.Asc()).All(ctx, db)

product, err := Products.Find(ctx, db, "SKU-1")
created, err := Products.Insert(ctx, tx,
	Products.Code.Set("SKU-1"), Products.Price.Set(100))
updated, err := Products.Update(ctx, tx, "SKU-1", Products.Price.Set(0))
page, err := Products.Query().Paginate(ctx, db, orm.Page(2, 20))
```

Omit an assignment to leave the field unwritten. `Set(0)` writes zero.
`SetNull()` is available only on nullable columns. Use `NullableTextOf`,
`NullableNumberOf`, `NullableTimeOf`, `NullableIDOf`, or `NullableColumnOf` for
`value.Nullable` fields.

Declare every persisted field. Tag transient fields with `foundry:"-"`.
Relation fields are slots and do not become columns in this prototype.
`foundry:"column=email_address"` overrides the SQL name, and
`foundry:"default=database"` permits omission on insert.

Use `EnumOf` for a generated enum descriptor and `BytesOf` for owned byte
buffers. Use `ColumnOf(b, &p.Field, orm.Codec(custom))` for a custom codec.

A persisted `CreatedAt` and `UpdatedAt` pair uses the existing timestamp
convention. Both fields must be `time.Time` or `temporal.DateTime`.
`orm.NoTimestamps()` disables that convention. A nullable instant named
`DeletedAt` enables soft deletion. Explicit timestamp constructors support
other field names.

Use `ForUpdate().All(ctx, tx)` or `ForShare().All(ctx, tx)` for row locks.
Their terminal methods require `*database.Tx`. Ordinary reads accept
`database.Executor`; writes accept `database.Transactor` and retain nested
transaction behavior. A `*orm.WriteError[M]` preserves a hydrated candidate when
transaction confirmation is uncertain. The candidate does not prove persistence.

Hooks and mutators remain outside this increment. Direct
relations, inner joins and projections are described below.

The key field is bound only by `TableOf`. Derive ordered numeric capabilities
with `orm.NumberKey(table.Key())` or text capabilities with
`orm.TextKey(table.Key())`; a second field binding is an error. Primary keys are
comparable, cannot use sensitive codecs, and a `model.ID[M]` key must belong to
its model. `Text` and `NullableText` retain named string types.

Numeric adjustments require a strictly positive, finite amount:

```go
product, err := Products.Update(ctx, tx, "SKU-1",
    Products.Price.Decrement(10, orm.AtLeast(int64(0))))
n, err := Products.Query().Where(Products.Price.Lt(500)).UpdateAll(ctx, tx,
    Products.Price.Increment(10, orm.AtMost(int64(500))))
```

The bound is checked by the same SQL UPDATE. A per-key guarded update returning
no row reports `orm.ErrBoundExceeded` wrapped in `fault.Conflict`; this includes
a missing key because the single statement cannot distinguish those causes.
Set-based writes return the number of rows satisfying the bound. Adjustments
preserve SQL NULL and accept one numeric adjustment plus ordinary assignments.
`validation.Positive[N]()` applies the same strictly positive, finite rule to
scalar numeric input; `validation.DecimalPositive()` checks exact decimal input.
Both rules currently run only on the server.

Construct bounds with `AtLeast` or `AtMost`; a zero-value `Bound` is rejected.
PostgreSQL evaluates `column +/- amount` before comparing the bound. An int64 or
float64 overflow returns a database out-of-range error rather than silently
passing the guard.

`InsertMany(ctx, tx, [][]orm.Assignment[M]{...})` inserts an atomic bounded batch.
`NoSoftDeletes()` disables the deletion convention and conflicts with explicit
`SoftDeleteOf`. `Table.ForceDelete(key)` physically deletes active records by
default; use package `ForceDelete` with a `WithTrashed` or `OnlyTrashed` query for
deleted records. `Query.ForceDeleteAll` respects query visibility. Explicit timestamp roles
win over names; explicit timestamp constructors conflict with `NoTimestamps()`.
`CreatedAt`, `UpdatedAt`, and `SoftDelete` retain the instant type as their second
type parameter. Upsert remains deferred.

`Query.Chunk(ctx, ex, size, func([]M) error)` reads bounded batches with stable
ordering and preserves Limit/Offset. Later batches advance from the last ordered
values captured before the callback. Nullable ordering uses the same keyset
predicate as the existing query API; explicit NULL placement falls back to
OFFSET. Rows close before callbacks run. `EachChunked` uses the same batches and
yields one model at a time. Callback errors and cancellation stop traversal.

## Relations and projected rows (Stage 1b)

Bind slots during `Define`, then declare relationships after both tables exist:

```go
var OrderRelations = struct { Buyer orm.BelongsTo[Order, User] }{
    Buyer: orm.BelongsToOf(Orders.Buyer, Orders.BuyerID, Users.ID),
}
orders, err := Orders.Query().With(OrderRelations.Buyer).All(ctx, db)
loaded, err := orm.Load(ctx, db, orders, OrderRelations.Buyer)
```

`OneOf` and `ManyOf` bind `relation.One` and `relation.Many` fields. Nullable
foreign IDs retain their non-null key type. The loader uses the shared engine's
500-key batches and 10,000-row fetched/attachment budgets. `Load` returns copies;
errors publish no partial result. Target soft deletes and context scopes remain
active. `Each` closes each parent batch before loading relations.

For joins, `rows.As[Tag]` creates a distinct scope, `expr.At` binds a model column
to that scope, and `rows.LeftScope`/`RightScope` move fields into the joined scope.
`rows.Define` returns a schema embedding `rows.Row[R]` from `b.Row()`;
`rows.IDOf`, `TextOf`, `NumberOf`, `EnumOf`, `TimeOf`, `BytesOf` and their
`Nullable...Of` counterparts take field pointers and choose their typed codecs.
Custom values use `rows.FieldOf(b, &r.Field, orm.Codec(custom))` or
`rows.NullableFieldOf` with the underlying value codec. `rows.Map` unifies
that field's value type with its scoped expression. `rows.Select` requires every
field once. `rows.SelectValue(Orders.Query(), Orders.TotalCents)` selects a scalar.
The complete example is [the reporting fixture](../../tests/fixtures/goway/reporting/orders.go).

Enum descriptors resolve lazily on `Check` and first use, after package
initialization. Model files can sort before generated enum files; no manual
initialization references are needed. Call `orm.Check` and `rows.Check` during
application startup to collect typed `fault.Invalid` declaration errors.

Scoped `expr.Expression[S,V,P]` keeps comparison value `V` separate from projected
value `P`. Nullable fields compare against `V` and project `value.Nullable[V]`;
use `IsNull`/`IsNotNull` for nullness. `rows.On` unifies comparable unwrapped keys,
and `rows.InnerJoin` accepts only an aliased table on its right side.

Column kinds declare their methods directly over shared, method-free state (R9).
`Number`, `Text`, `Time`, `Enum`, keys and nullable kinds retain their method completion
lists and value receivers, including calls on `table.Key()` and derived key
handles returned by value. Private relation adapters carry state without returning
another public column type. Codec-state construction is shared by value type;
model markers stay on the public handles and private interface arguments.
The [surface test](method_surface_test.go) pins the
exported fields, consumer-visible method signatures and sealed interface methods; fixture budgets cover individual kinds and growth
from one to three identically shaped models.

Stage 3.0 deliberately removes the formerly promoted exported `.Column` field
(including `Orders.ID.Column` and `orm.TextKey(k).Column`) to remove embedding
chains. Exported methods remain unchanged; this is not preservation of the entire
public surface. No consumer migration is needed on this branch.

Stage 3.1a adds `JSONOf`/`NullableJSONOf` for `value.JSON[T]` and
`IntervalOf`/`NullableIntervalOf` for `temporal.Interval`. JSON handles keep the
payload type through equality, membership, containment and assignment. They expose
`Asc`/`Desc` and `IsJSONNull`, but no range comparisons or text patterns. `Kind`
and `Length` expressions remain Stage 6d. Interval handles add ordered comparisons
and sorting, without numeric adjustments or text patterns. Nullable handles distinguish SQL NULL from
values; JSON null is represented by `value.JSON[value.Nullable[T]]`.

Use `TableOf(b, &m.Period, orm.Codec(codec.Interval()))` for an interval natural
key. PostgreSQL compares intervals using 30-day months and 24-hour days: an
`Eq(temporal.Months(1))` predicate also matches stored `30 days`. Interval natural
keys therefore collide for `1 mon` and `30 days`; hydration preserves their distinct
calendar and elapsed components. Interval `Sum`/`Avg` capabilities remain Stage 6a.
`BytesOf`/`NullableBytesOf` retain buffer ownership and distinguish empty bytes
from NULL. `Text.IContains` and `NullableText.IContains` treat `%`, `_` and `\`
as literal substring characters and match case-insensitively. For example,
`Users.Email.IContains("@EXAMPLE.com")` binds a value; it never interpolates SQL.
Stage 3.1b adds `PasswordOf`/`NullablePasswordOf`, which preserve typed hash
scalar comparisons, sorting and assignment while rejecting plaintext and text
patterns. The password codec retains its sensitivity and redaction.

`EncryptedTextOf`/`EncryptedJSONOf` and their nullable constructors expose typed
`Set`; nullable handles also expose `SetNull`, `IsNull` and `IsNotNull`. They do
not support ordering, value comparisons or row projections. Single-model writes
seal after UUID generation, inside the transaction using its keyring. Every
set-based encrypted assignment is rejected, even with a primary-key equality.
Hydration opens using each row's table, column and key through the same shared
owner as the generated API. Failures publish no partial model. Table/column/key
renames require re-encryption; see [encryption](../../docs/guides/encryption.md).
Unknown or committed callback failures retain reconciliation candidates through
`WriteError`; a candidate never proves successful commit.

Encrypted Go values (including `value.Nullable` wrappers) must use the
`EncryptedTextOf`/`EncryptedJSONOf` constructors or their nullable forms.
`ColumnOf` with an explicit encrypted codec is rejected at startup. Row
projections reject encrypted values because they cannot open them yet.

Hydration publishes only complete, successfully opened models. Buffered reads,
page reads, eager loads, and each chunk discard the failing batch. Streaming
`Each` retains rows already delivered before an error; it never delivers the
failing row or any later row. This matches the old API's streaming behavior.

Stage 3.1c adds cursor pagination and primary-key chunking as package functions:

```go
page, err := orm.CursorPaginate(ctx, db,
    models.Users.Query().OrderBy(models.Users.Nickname.Asc()), request.Page)
// request is pagination.CursorRequest[Path, Filters, models.User].
// Map the returned query.CursorPage with pagination.MapCursorPage.
err = orm.ChunkByKey(ctx, db, models.Orders.Query(), models.Orders.ID, 500,
    func(batch []models.Order) error { return process(batch) })
err = orm.EachByKey(ctx, db, models.Orders.Query(), models.Orders.ID, 500, processOrder)
```

Cursor requests/results reuse the existing HTTP pagination adapters and
`data`/`meta`/`links` JSON shape. Tokens retain the old unsigned v1 format and
concrete model, filter and ordering scope. They are positions, not credentials: a
client may move its own position, but query predicates (tenant, soft-delete and
scopes) always still apply. Nullable
and mixed orders use the same keyset predicate, with the primary key appended as
a unique tiebreaker. Existing Limit/Offset windows are rejected.

Key chunks accept only primary-key ordering and zero Offset; Limit bounds total
delivery. Rows close before callbacks and the next key is captured before callback
edits. Deleting rows behind the boundary does not shift later batches. Keep keys
stable; iteration does not provide a snapshot across callbacks.

Locked keyed reads compose `Orders.Query().Where(Orders.ID.Eq(id)).ForUpdate().Require(ctx, tx)`.
The executor remains `*database.Tx`; unlocked reads retain read-pool routing.

## Typed raw hydration

`Table.Raw` and `rows.Raw` apply **no automatic scopes**: callers must write tenant,
soft-delete and other scope predicates themselves. Both use the supplied executor
exactly; raw SQL may write, so they do **not** choose a read replica. For explicit
read routing, the existing level-1 `db.QueryRead` or `database.ReadQuery` API is
available; an executor wrapper can deliberately forward `Query` to `QueryRead`
when its SQL is known to be read-only. There is no implicit SQL classification.

```go
orders, err := Orders.Raw(ctx, tx,
    "SELECT * FROM orders WHERE id=$1", id)
report, err := rows.Raw(ctx, db, OrderRows,
    "SELECT id, label FROM orders WHERE id=$1", id)
```

Results must contain exactly the declaration's column names, in any order. Use
SQL aliases for declared names. Missing, duplicate or extra names fail even when
there are zero rows. Arguments go unchanged to the executor. Existing codecs and
model encryption opening are reused; row schemas still reject encrypted types.
Any decoding/opening failure discards the whole result and closes the stream.
For `UPDATE … RETURNING`, a hydration error does not prove rollback: use an owning
transaction and preserve the executor's outcome classification.

## Direct eager trees and existence

Declare `BelongsToOf`, `HasOneOf` or `HasManyOf` once, then derive immutable target
constraints and finite trees:

```go
orders, err := Orders.Query().With(
    OrderRelations.Buyer.Where(Users.Age.Gt(18)).With(UserRelations.Orders),
).All(ctx, db)

q := Users.Query().Where(orm.Has(UserRelations.Orders, Orders.TotalCents.Gt(100)))
q = orm.WithRelationLimits(q, orm.RelationLimits{
    BatchSize: 100, MaxRows: 5000, MaxDepth: 4,
})
missing, err := orm.LoadMissing(ctx, db, users, UserRelations.Orders)
```

Each query load executes one parent statement plus, for each tree branch,
`ceil(distinct eligible non-null keys / batch size)` statements. Nested branches
batch the complete fetched target collection. `Load` and `LoadMissing` issue no
parent statement; the latter preserves already-loaded slots, including empty
ones. Supplied parent provenance/authorization belongs to the caller. Related
soft-delete and declared scopes still apply. `Has`/`DoesntHave` are correlated
EXISTS predicates, compose recursively, and add zero statements or eager loads.

Defaults come from the shared relation-limits owner: batches of 500, depth 8,
and a shared maximum of 10,000 fetched rows and 10,000 attached values across the
whole tree. Query overrides apply to every branch, pagination and each chunk
window. Invalid graphs fail before parent SQL; budget/cardinality/decoding errors
publish no partial collected parents. Singular relations reject multiple matches.
Relation `Limit`/`Offset` is unavailable: a global batch limit would truncate
collections across parents. Target ordering appends the primary key as a tie-breaker.

For a statement-count regression, install one detector on the pool and begin a
scope around the read. Set a threshold appropriate to the expected batch count:

```go
detector, err := database.NewStatementDetector(database.StatementDetectorConfig{
    Threshold: 4, MaxShapes: 32,
})
// Handle err; supply database.WithStatementDetector(detector) when opening db.
ctx, scope := detector.Begin(t.Context())
_, err = Users.Query().With(UserRelations.Orders).All(ctx, db)
// Handle err, then check both the operation's exact statement count and shapes.
dbassert.AssertStatementShapes(t, scope)
```

Close rows before checking. Only compiler-produced shapes appear as SQL text;
raw statements use keyed fingerprints and violations include bounded call sites.
See [statement instrumentation](../../docs/guides/database-runtime.md#detecting-repeated-statement-shapes).

## Joined relation families

`ThroughOf(b, &m.Groups)` binds a `relation.Through[Group, Membership]` slot.
`ManyToManyOf` unifies the source/pivot and pivot/target key pairs without retaining
public generic queries in the descriptor:

```go
links := orm.ManyToManyOf(Users.Groups, Users.ID,
    Memberships.UserID, Memberships.GroupID, Groups.ID)
users, err := Users.Query().With(
    links.Where(Groups.Active.Eq(true)).WherePivot(Memberships.Role.Eq("admin")).
        With(GroupRelations.Owner).WithPivot(MembershipRelations.Inviter),
).All(ctx, db)
for _, user := range users {
    for _, link := range user.Groups.All() {
        use(link.Model, link.Pivot)
    }
}
```

Targets and pivots remain separate typed values in each `relation.Link`.
Distinct pivot rows remain distinct links. A pivot matching multiple targets fails
with `database.TooManyRows`; NULL keys, missing targets and filtered targets create
no link. Target and pivot soft-delete scopes apply independently. Use
`WithTrashedPivot`, `OnlyTrashedPivot` or `WithoutTrashedPivot` to change the latter.
`OrderBy` and `OrderByPivot` retain their combined call order, with target and pivot
primary keys appended as tie-breakers.

`HasManyThroughOf` and `HasOneThroughOf` accept a slot followed by four columns:
source local key, intermediate first key, intermediate second key, target foreign
key. Both key pairs and the intermediate owner must match. Intermediates retain
their scopes. A singular descriptor's `OfMany(orders...)`, `LatestOfMany()` and
`OldestOfMany()` select one target per parent through `DISTINCT ON`, with a primary
key tie-breaker. `OfMany` requires an order; latest and oldest use the primary key.

`MorphName` and `Morphable` reuse the old API's persisted discriminator contract.
`MorphOneOf` and `MorphManyOf` filter targets by the source's declared name.
`MorphToOf` declares one concrete slot per possible target and loads only matching
source rows. `MorphToManyOf` and `MorphedByManyOf` constrain the pivot discriminator.
Names must follow the identifier grammar. Discriminator values are bound parameters.

Each joined family uses one SELECT per key batch. Nested target and pivot branches
batch their complete collections under the same depth and fetched/attached-value
budgets. Each attached link consumes two values. Polymorphic inverses batch each
concrete discriminator group; an empty eligible key set sends no statement.
`Has` and `DoesntHave` also accept these descriptors without extra round trips.
Relation writes and pivot lifecycle remain Stage 5b.
## Field mutators and explicit accessors

`orm.Check` reports invalid declarations before application resources start.
`Mutate<Field>(Input) (Stored, error)` and `Access<Field>() (Result, error)` use
value receivers and exact concrete types. Their suffix must name a persisted Go
field, including after a rename. Binding happens once during `Define`; writes
invoke typed functions without reflective dispatch. Mutators use a zero receiver,
so use the input argument rather than receiver state. Accessors are ordinary
explicit calls and never replace stored hydration or DTO mappings.

For a same-type transformation, either declare the method or pass an option:

```go
Email: orm.TextOf(b, &m.Email, orm.Mutate(normalizeEmail)),
```

For distinct input and storage types, declare an input column:

```go
Email: orm.InputTextOf(b, &m.Email, normalizeEmailInput),
Note:  orm.NullableInputTextOf[Member, LabelInput](b, &m.Note, nil),
```

A nil constructor transform selects the matching `Mutate<Field>` method. An
explicit transform and a named mutator for the same field are a declaration
error. `InputColumn`, `InputText`, `InputNumber`, `InputTime`, `InputEnum`,
`InputJSON` and `InputInterval` retain their stored comparison, ordering and
expression capabilities. Each also has a nullable form. `InputBytesOf` uses an
owned binary codec with the `InputColumn` kind. `Set` takes only the concrete
input type; predicates take only the stored type. Nullable `SetNull` keeps SQL
NULL distinct from an omitted assignment or an assigned zero value.

Mutators run in declaration order inside the write transaction, after automatic
timestamps and before encryption and binding. They run once per assigned value,
including values in a batch or a set update. Omitted fields and explicit SQL
NULL skip scalar callbacks. Reusing assignments starts from the original input;
direct binary inputs own their buffers. Other custom inputs retain ordinary Go
copy semantics. Return an error to abort the transaction or savepoint. Panic and
Goexit use the existing transaction isolation boundary. Keep transforms pure and
free of I/O. Encrypted fields reject mutators; transform plaintext before creating
the encrypted value. SQL adjustments cannot bypass a field's Go mutator.

The independent [mutator consumer](../../tests/fixtures/goway/models/mutators/models.go)
and its [PostgreSQL source](../../tests/fixtures/goway/pgsmoke/mutators_test.go)
exercise distinct inputs, explicit getters, omission, NULL and rollback.

Primary keys with distinct mutation inputs reuse the one primary declaration:

```go
table := orm.TableOf(b, &user.ID)
id := orm.InputKeyOf[User, FreshID](b, table.Key(), nil)
```

`id.Set(FreshID{Value: storedID})` takes fresh input. `id.Eq(storedID)` and
`table.Find(ctx, db, storedID)` retain the stored key type. Declare this adapter
inside `Define`. Its nil transform selects the matching named method. A distinct
input disables automatic UUID creation, so an omitted required key fails before
SQL. Ordinary same-type UUID key mutators retain automatic UUID preparation.

Mutator callback errors expose only a fixed field diagnostic through ordinary
formatting. Their original cause remains available through `errors.Is` and
`errors.As`. Required and nullable password constructors bind named mutators;
encrypted fields continue to reject them.

### Write lifecycle

Declare a per-write factory with `WithHooks(func() Hooks[Order] { ... })` as a
`Define` option. The factory runs inside the owning transaction, after a locked
original has been hydrated and its rows closed. It runs once per normal write.
`Saving` precedes `Creating` or `Updating`. Mutators and conventions follow these
callbacks. The effective draft is validated before mutators and encryption;
omitted required fields can be supplied with
`WriteSet(write, Orders.TotalCents.Set(0))`.

Draft getters accept typed setter functions. `WriteGet(write, Orders.TotalCents.Set)`
returns `Optional[int64]`; `WriteNullableGet(write, Members.Note.Set)` returns
`Optional[Nullable[LabelInput]]` when `Note` has a distinct `LabelInput` mutation
input. `WriteIsSet(write, Members.Note.Set)` also reports an explicit NULL as
assigned. These functions preserve input values without running mutators. Use the
nullable getter for nullable fields. Getter/assignment errors abort the draft even
if a callback ignores them. Draft pointers are valid only during before hooks.

After RETURNING has hydrated and closed its rows, `Created` or `Updated` precedes
`Saved`. Deletion runs `Deleting` then `Deleted`; force deletion adds
`ForceDeleting` before `Deleting` and `ForceDeleted` after `Deleted`. Restore runs
`Restoring` then `Restored`. A callback error, panic, Goexit or cancellation aborts
the owning transaction/savepoint. Hook and comparison errors expose fixed framework
text while retaining the original cause for `errors.Is`/`errors.As`. The supplied
context shares the generated API's
32-level lifecycle nesting bound; propagate it to nested operations.

`Changes[Order]` contains only persisted fields, with locked originals and actual
RETURNING results (including defaults, triggers and managed timestamps).
`ChangeOf(changes, Orders.TotalCents)` returns the existing
`lifecycle.FieldChange[int64]`; `NullableChangeOf(changes, Members.Note)` retains
`Nullable[StoredLabel]`. `Assigned` reflects effective assignments, even if the
canonical stored value did not change. `Before` is absent on create; `After` is
absent after physical deletion. Snapshot getters return owned binary buffers and
exclude ignored fields and relation slots. `Compare(Orders.Table, before, after,
assignments...)` provides the same comparison without a write or operation tag.

Observers use `NewObserver[Order]("orders.audit")` and `RegisterObserver` through
the existing database Module/provider registration at `application.New`.
Dependency constructors return `func() Hooks[Order]`. Each application's pool owns
an immutable observer set. At every stage local hooks run first, followed by
observers in declaration order, including models without a local factory.
`NewDeletionObserver` limits dispatch to deletion operations.

`AfterCommit` callbacks merge through successful savepoints, are discarded on
rollback, and run after the outer commit releases its connection. All callbacks
run even if one fails; failures preserve the committed outcome and reconciliation
candidate. Use the transaction supplied to after hooks for durable outbox/audit
writes: in-memory after-commit work is not crash durable.

Set writes skip per-model hooks, observers and change capture. A declaration with
hooks or observers requires `WithoutModelHooks(Orders.Query()).UpdateAll(...)`
(and likewise delete/force-delete); unknown executor wrappers require this explicit
acknowledgement too. Per-model writes reject the acknowledgement. `InsertMany`
retains its existing bulk semantics: no per-model hooks/observers.
Observer discovery shares the generated API's concrete framework-owner lookup;
it never invokes a wrapper's observer method or gives it an internal capability.
Per-model writes through wrappers dispatch observers from the supplied transaction.

`Orders.ForceDelete(ctx, db, id)` preserves default active visibility. For deleted
rows use `ForceDelete(ctx, db, Orders.Query().WithTrashed(), Orders.Key(), id)`;
`OnlyTrashed` and other predicates are retained through locking and deletion.

## Typed relation aggregate slots

Declare a non-persisted `relation.Value[V]` field on the model and an
`orm.Value[Model,V]` handle on its table. Bind the handle inside `Define` with
`orm.ValueOf(b, &model.Count)`. Like relation slots, aggregate slots cannot carry
persistence tags and do not enter write drafts or ordinary row hydration.

Bind computations after tables and relations have initialized:

```go
var UserAggregates = struct {
    OrdersCount orm.AggregateRelation[User, int64]
    OrdersTotal orm.AggregateRelation[User, value.Nullable[decimal.Decimal]]
}{
    OrdersCount: Users.OrdersCount.Using(
        expr.Related(UserRelations.Orders, expr.Count[Order]())),
    OrdersTotal: Users.OrdersTotal.Using(
        expr.Related(UserRelations.Orders, expr.Sum(Orders.TotalCents))),
}

users, err := Users.Query().With(UserAggregates.OrdersCount).All(ctx, db)
count := UserAggregates.OrdersCount.Using(expr.Related(
    UserRelations.Orders.Where(Orders.TotalCents.Gt(minimum)),
    expr.Count[Order](),
))
users, err = orm.LoadMissing(ctx, db, users, count)
```

Keep dependencies flowing from tables to relation namespaces to aggregate
namespaces. Table initializers must not reference relation or aggregate namespaces;
relation initializers must not reference aggregates. Compose nested eager loads at
use sites, for example `OrderRelations.Buyer.With(UserAggregates.OrdersCount)`.
Wrapping a reference in a closure does not remove a Go initialization cycle.

`expr.Related` preserves the source model, measured model and exact aggregate result.
`Using` retains the destination slot when replacing its computation. Computations
without slot binding fail validation before execution. Different declarations of
the same model remain distinct owners and cannot be mixed.

The [expression aggregate rules](expr/README.md) apply unchanged: counts return
`int64`, existence returns `bool`, exact sums/averages return nullable decimal,
float summaries return nullable float64, interval summaries return nullable interval,
and extrema retain the present field type with one nullable layer. Use the existing
`SumFloat`/`AvgFloat` and `SumInterval`/`AvgInterval` constructors for those families.
`Filter` takes expression row predicates, for example
`expr.Sum(Orders.TotalCents).Filter(expr.OrderRow(expr.Value(Orders.TotalCents)).Gt(0))`.
It filters the measure without hiding cardinality errors in the input relation.
Relation `Where` restricts that input before aggregation.

A joined relation measures targets. `orm.Pivot(UserRelations.Groups)` measures
its concrete pivot, retaining target and pivot filters and soft-delete visibility.
Counts include duplicate edges; use `expr.CountDistinctField(Groups.ID)` for distinct
targets. Missing targets and NULL join keys contribute no edges. Singular relations
still reject multiple matching rows; many-to-many relations reject a pivot matching
multiple targets. Direct, through and concrete morph families are supported.
Aggregate sources must be framework descriptors: zero, nil and application wrappers
(including embedded `AggregateSource` interfaces) fail validation before SQL.
One-of-many and MorphTo inputs are rejected at plan validation. Inputs requesting
eager target or pivot children are also rejected; load those as separate branches.

Loading uses existing `With`, `Load` and `LoadMissing`, including nested target and
pivot branches, transactions, locked reads and chunk windows. Each selected slot
executes one grouped statement per batch of distinct eligible non-null source keys.
Slots are not fused. A query adds its parent statement; explicit loads add none.
Empty key sets execute no aggregate SQL. Relation ordering is omitted from grouped
SQL. Shared row and attachment limits apply: each returned group consumes one row,
and each computed parent slot consumes one attachment, including empty results.
These limits bound returned data, not the rows examined by the database.

`Get()` returns the typed value and a loaded flag. A loaded zero/false/SQL NULL is
preserved by `LoadMissing`; `Load` refreshes it. No related models are hydrated to
compute a summary. Errors or cancellation discard the collected result and leave
caller-owned parents untouched. All statements use the supplied executor; choose
transaction isolation when separate slots require one consistent snapshot.

## Complete-model retrieval and global scopes

`WithRetrievalHooks(func() RetrievalHooks[Order] { ... })` declares a separate
read factory. `Retrieved(ctx, executor, order)` runs after complete stored
hydration and successful row closure. Each nonempty fetch/batch constructs the
local factory first, then retrieval observer factories in registration order;
each model invokes local callbacks before observers. Register instance-owned
read observers with `NewRetrievalObserver[Order]("orders.read")` and
`RegisterRetrievalObserver`, using the existing database Module at
`application.New`. Write and retrieval observer kinds remain distinct.

Reads use the actual rows' frozen observer owner. Unknown executor wrappers
cannot prove absence and receive no internal capability. `Each` keeps streaming
when callbacks cannot run; otherwise it reads bounded, stable, closed batches
before callbacks and delivery. `All`, pagination, chunks and loaded relation
roles dispatch retrieval. A failed batch publishes no pending models. Earlier
delivered batches remain delivered. Write hydration, DTO projections and scalars
skip retrieval, even when their source has read callbacks. `Table.Raw` dispatches
retrieval for its complete models while bypassing all scopes and read routing.
A callback failure does not roll back SQL supplied to Raw.

Attach tenant defaults through a lazy source receiving the completed typed
declaration. This avoids Go initialization cycles and snapshots declaration
handles before consumers can replace their public fields:

```go
var Orders = orm.Define("orders", declareOrders,
    orm.WithGlobalScopes(func(t OrderTable) []orm.GlobalScope[Order] {
        return []orm.GlobalScope[Order]{
            orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[Order], error) {
                id, err := tenantID(ctx)
                return t.TenantID.Eq(id), err
            }),
        }
    }),
)
```

The source runs once when first needed. It may construct relation predicates but
must not compile or execute queries. Each scope returns an additive predicate
and an error; a zero predicate, foreign declaration, missing context/tenant,
invalid name, duplicate name or callback failure fails closed before SQL.
Source failures, including panic and Goexit, are retained by the declaration;
every later scoped operation returns the failure without executing SQL.
Context values are explicit typed request metadata. There is no service registry
in the context. Every terminal uses its executing context; `WithScopeContext(q,
ctx)` only supplies context for `Compile` and inspection.

Scopes apply through the shared effective-predicate owner to reads, keyed and
set writes, relation targets/pivots, aggregates, joins and subqueries. Inserts
are unfiltered. Remove defaults for one query with `WithoutGlobalScope(q,
scope)` or `WithoutGlobalScopes(q)`; explicit predicates and soft-delete
visibility remain. Apply these before taking a row lock. Relation package
functions preserve the concrete descriptor and its other side's defaults:
`WithoutRelationGlobalScope`, `WithoutRelationGlobalScopes`,
`WithoutPivotGlobalScope` and `WithoutPivotGlobalScopes`. Bypasses do not mutate
the source query, descriptor or declaration. Treat them as explicit authorization
choices, especially for tenant scopes.
