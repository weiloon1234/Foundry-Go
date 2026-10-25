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

The bound is checked by the same SQL UPDATE. Per-key adjustments return
`orm.ErrBoundExceeded` wrapped in `fault.Conflict` for a bound-rejected visible row,
and `database.NotFound` for a missing or hidden key, with or without hooks/observers.
An empty guarded write or lifecycle locking read adds one key-only locking read
in the same transaction, retaining all visibility predicates except the bound.
Successful writes keep their statement count; see the [batch-write guide](../../docs/guides/model-batch-writes.md).
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
`SoftDeleteOf`. `Table.ForceDelete(ctx, db, soft, key)` physically deletes active records by
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
missing, err := orm.LoadMissingFrom(ctx, db, q.With(UserRelations.Orders), users)
```

Each query load executes one parent statement plus, for each tree branch,
`ceil(distinct eligible non-null keys / batch size)` statements. Nested branches
batch the complete fetched target collection. `Load` and `LoadMissing` issue no
parent statement; the latter preserves already-loaded slots, including empty
ones. `LoadFrom` and `LoadMissingFrom` accept a query with `With` relations and
`WithRelationLimits` overrides. They validate that query without fetching or
filtering the supplied parents; parent predicates, order and window do not change
the input collection. All four functions share the same copy-and-load engine. Supplied parent provenance/authorization belongs to the caller. Related
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

For a same-type transformation, use an input column with a typed transform:

```go
Email: orm.InputTextOf(b, &m.Email, normalizeEmail),
```

Use the same input-column family for distinct input and storage types:

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

`orm.WriteUnset(write, Users.EmailRevision.Set)` removes the pending assignment in
Saving/Creating/Updating. Getters then report omission, including after an explicit
zero or SQL NULL. It does not assign a zero or NULL, invoke mutators, or undo a
previously persisted value. Unsetting an omitted field is harmless. Later hooks can
set it again; automatic defaults/timestamps still follow their ordinary conventions.
The setter keeps the model and input types; foreign declarations and expired drafts
fail validation. Caller-owned assignment slices remain reusable.

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

`Orders.ForceDelete(ctx, db, Orders.Soft, id)` preserves default active visibility. For deleted
rows use `ForceDelete(ctx, db, Orders.Query().WithTrashed(), Orders.Soft, Orders.Key(), id)`;
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

## Audit capture and history

Declare model-owned policy with `orm.AuditPolicyOf(Orders.Table,
 orm.AuditField(Orders.Note, record.Exclude))`. Unspecified fields use
`record.Automatic`; `record.Redact` retains change flags and hides values.
A zero `orm.AuditPolicy[Order]` applies Automatic everywhere. Policies cannot
exclude or redact the primary key while retaining it as the subject identity.
Foreign columns, duplicate rules and invalid disclosures fail registration or
capture. Encrypted fields use `AuditEncryptedField` or
`AuditNullableEncryptedField` for explicit exclusion; Automatic redacts them.

Register `orm.Auditing(Orders.Table, policy)` through
`audit.Register(r, pool, recorderKey, config, declaration)` in an application
module. The recorder and write observer belong to that application instance.
Created, Updated, Deleted and Restored capture inside the business transaction,
after local callbacks. Force deletion uses Deleted once. An audit failure or a
later write failure rolls back both business and audit rows. Set writes still
require explicit `WithoutModelHooks` acknowledgement and produce no automatic
audit entries. Install audit storage through the existing audit migrations.

`orm.Audit(Orders.Table, changes, policy)` also captures a lifecycle callback's
stored changes without SQL. Standalone `Compare` results cannot be audited.
Updates, soft deletes and restores retain assigned or changed fields plus the
key; creates and physical deletes retain complete snapshots subject to policy.
Values come from locked originals and RETURNING, after mutators and database
changes; accessors never replace them. Sensitive codecs, encrypted fields and
sensitive names are redacted before their capture binder runs. Nested JSON keys
are redacted by the shared record owner before binding the audit row. Unchanged
and excluded fields do not invoke their capture codec.

History remains `record.Model[M,K]`; use `orm.AuditRead(history, Orders.Total)`
or `orm.AuditReadNullable(history, Orders.Note)` with stored column types.
`AuditReadEncrypted` and `AuditReadNullableEncrypted` expose redaction state.
An omitted field, an absent model snapshot and SQL NULL remain distinct.
Redacted fields and partially redacted JSON cannot hydrate fabricated values;
inspect `Snapshot()` for deliberate export. The same declared codecs restore
persisted keys and historical field values. Audit adapters are package functions
and add no methods or per-column closures to ordinary reads.

## Pivot relation writes

Relation writes are package functions; `ThroughRelation`, `Query` and `Table`
keep their existing method sets. Inputs and results retain the concrete source,
target and pivot types:

```go
pivot, err := orm.Attach(ctx, db, UserRelations.Groups, user, group,
    Memberships.Role.Set("member"))
changes, err := orm.Sync(ctx, db, UserRelations.Groups, user, groups,
    []orm.Assignment[Membership]{Memberships.Role.Set("member")}, nil)
removed, err := orm.Detach(ctx, db, UserRelations.Groups, user, group)
```

`AttachMany`, `SyncWithoutDetaching`, `Toggle`, `DetachMany`, `DetachAll` and
`UpdatePivot` cover bounded bulk operations. `Sync` accepts separate create and
update assignment slices; a nil update leaves retained links unchanged. Endpoint
keys and morph discriminators fill omitted assignments. Explicit assignments and
hooks may change them, but inserted pivots must satisfy the relation's endpoint
and filter postconditions. Attach permits duplicate existing links; constraints
remain the migration's responsibility. Target inputs are deduplicated by refreshed
relation keys in input order. Nullable keys use their stored value type even when
assignment inputs differ. If a pivot key or morph discriminator has a distinct
mutator input, supply that typed assignment explicitly: its stored key cannot
synthesize an input. Missing distinct inputs fail before pivot hooks and mutation
SQL, matching the old API. Explicit assignments and hook replacements run the
mutator once; typed draft getters retain the original input type.
Single Attach rechecks both original endpoint identities and their scopes after
pivot hooks, inside the same transaction.

Writes refresh and share-lock endpoint identities, skip retrieval callbacks and
retain global scopes, target filters, pivot filters and soft-delete visibility.
Bulk operations that read current links take `FOR NO KEY UPDATE` on the source to
serialize competing changes. Existing links are locked in primary-key order.
Like the old API, bulk sync/detachment selects existing links by pivot filters;
target filters constrain requested endpoints and inserted-link checks.
`Detach` soft-deletes when declared; `ForceDetach` requires a soft-delete pivot
and preserves selected pivot visibility, including `WithTrashedPivot()`.

Pivot hooks or observers select the ordinary per-model B5b lifecycle. Otherwise,
create/update/removal uses one set statement per change kind. No per-row savepoints
are added: each operation owns one transaction, or one savepoint inside the caller's
transaction. With primary endpoint keys, attachment uses four statements for any
hook-free batch (source, targets, INSERT, postcondition). Sync adds a current-links
read and one statement for each nonempty update/removal; inserts also add their
postcondition. Hooked inserts cost one statement per pivot; hooked updates/removals
also select candidates and lock each original. Internal reads do not call Retrieved.

`WithRelationWriteLimit(links, n)` lowers the existing atomic row bound. Oversized
inputs and current/candidate sets fail; set RETURNING rows are bounded and an excess
rolls back the operation with `fault.Invalid`. Endpoint cardinality violations
remain `database.TooManyRows`. Ordering and eager-child options are rejected on write
relations. Empty target batches still refresh the source, as the old API does.

Errors never return successful partial results. Unknown commit and committed
callback failures preserve `WriteError[P]`, `WriteError[[]P]` or
`WriteError[PivotChanges[P]]` according to the function's result. `Candidate()` is
reconciliation data, not proof of persistence or permission to retry. After-commit
callbacks remain owned by the outer transaction.

## Conflicts and upserts

Upserts use package functions and an explicit immutable conflict policy:

```go
policy := orm.OnConflict(orm.ConflictKey(Users.Email)).DoUpdate(
    orm.ConflictIncoming(Users.Name), // EXCLUDED, already transformed
    orm.ConflictNull(Users.Nickname),
)
item, err := orm.Upsert(ctx, db, Users.Query(), policy,
    Users.Email.Set("ada@example.test"), Users.Name.Set("Ada"))
```

Conflict captures are framework-owned. `ConflictSet` accepts declared columns;
application embedding wrappers cannot replace their setters. `ConflictNull` uses
sealed nullable metadata and never calls an application `SetNull` override.

`ConflictSet(column, input)` accepts the column's ordinary write input, including
its distinct mutator input. Literal updates transform once per policy execution;
`ConflictIncoming` copies the destination's own proposed value without transforming
it twice. Omitted proposed fields use database defaults or NULL. Encrypted fields
cannot be conflict update targets. `OnConflictConstraint[User]("users_email_key")`
selects a named constraint. PostgreSQL owns constraint/index existence.

`UpsertMany` accepts a bounded batch of assignment slices. Both APIs skip local
hooks, observers, retrieval callbacks and automatic audit, matching the generated
upsert API; `WithoutModelHooks` is unnecessary. Mutators, encryption of proposed
insert fields, timestamps and complete RETURNING hydration still apply. Managed
update timestamps copy the normalized proposed instant. A skipped conflict has no
returned model; batch result order does not map results to input positions.

`policy.Where(...)` conditions the update using the stored row. Destination global
scopes also constrain DO UPDATE. An unconditional update suppressed by those scopes
returns a conflict error and rolls back the whole batch. Inserts remain unfiltered.
Conflicting repeated update targets are PostgreSQL errors. Caller transactions use
the existing savepoint behavior; reconciliation failures retain typed candidates
in `WriteError`. Column/constraint targets ship in slice 6e1; calculated index targets
and stored/proposed row calculations belong to the next slice.

Advanced conflict expressions use `orm/rows` and the existing `orm/expr` functions:

```go
records := rows.ConflictRowsOf(Products.Query())
stored := expr.At(records.Stored(), Products.Price)
proposed := expr.At(records.Proposed(), Products.Price)
policy := rows.OnConflictKeys(expr.Group(expr.Value(Products.Code))).
    TargetWhere(Products.Price.Gt(0)).
    DoUpdate(rows.SetConflictValue(Products.Price, expr.Add(stored, proposed)))
policy = rows.ConflictWhereRows(policy, expr.Greater(proposed, stored))
```

`OnConflictKeys` accepts row-phase index expressions. `TargetWhere` (or
`rows.ConflictTargetWhere` for computed predicates) describes a partial index;
its fixed migration values are rendered as schema constants. Ordinary insert
inputs, update calculations and conditions remain bound parameters. PostgreSQL
owns physical index compatibility. Subqueries and selected-phase expressions
cannot be index targets.

`ConflictRowsOf` exposes complete stored and proposed records in a distinct
`rows.ConflictRow[M]` scope. Query filters do not become conflict conditions.
The proposed record includes database defaults and trigger changes. Conflict
rows cannot execute or serve as standalone SELECT sources. Use `rows.Correlate`
with them for correlated scalar calculations and existence conditions;
`rows.ConflictScalarRowQuery` and `rows.ConflictScalarNullableRowQuery` handle
independent scalar selections. Source declarations and global scopes remain
validated, and missing tenant context fails closed before SQL.

`SetConflictValue` requires a declared destination and its exact stored type,
including nullability. Use `expr.NullableRow` to widen a required result.
Encrypted destinations remain excluded. A field with a Go mutator accepts only
its own proposed value as a calculation; other SQL calculations cannot bypass
that mutator. Upsert lifecycle, transaction and bounded RETURNING behavior are
unchanged from the literal conflict API.

## Insert-select and source writes

Source writes use `orm/rows` package functions and immutable builders. Source
expressions keep their scope and exact stored type, including nullability.
Mapping, match-key and operation types cannot be converted into one another:

```go
source := rows.From(Products.Query().Where(Products.Price.Gt(0)).Limit(100))
insert := rows.InsertFrom(source, Products.Query(),
    rows.MapInsert(Products.Code, expr.Value(Products.Code)),
    rows.MapInsert(Products.Price, expr.Value(Products.Price)))
inserted, err := insert.Returning(ctx, db, 100)

matched := rows.MatchSource(Products.Key(), expr.Value(Products.Code))
update := rows.UpdateFrom(Products.Query(), source).Match(matched).
    Select(rows.MapUpdate(Products.Price, expr.Value(Products.Price)))
count, err := update.Exec(ctx, db)
count, err = rows.DeleteUsing(Products.Query(), source).Match(matched).Exec(ctx, db)
```

`Values(assignments...)` replaces fixed inputs and normalizes them once inside
the owning transaction. SQL mappings cannot bypass Go mutators, copy encrypted
envelopes, or replace a managed update timestamp. Updates cannot assign primary
keys. Literal encrypted inputs are rejected for these set-based writes.

Omitted insert fields keep database defaults or NULL. Required fields must be
mapped or supplied; UUID keys require a source/literal value or database default.
No single Go-generated UUID is shared across selected rows. Managed creation time
is supplied only when omitted; update time always comes from the owning clock.

Source filters, windows and tenant scopes remain intact. Destination scopes apply
to updates and deletes; inserts are unfiltered. Missing tenant context fails
closed. `MatchNullableSource` allows nullable keys; NULL matches no destination.
Mapped updates require at most one source match per affected model and roll back
on ambiguity. Literal-only updates and deletes tolerate repeated source keys.

`Exec` returns an affected count. `Returning` accepts a limit from 1 through
`query.MaxInsertRows` (1000), hydrates complete models, and rolls back on overflow,
decode failure or row-close failure. The limit bounds retained models, not SQL
work or field sizes; it neither truncates the source nor promises result order.

These explicit set-based APIs skip local hooks, observers, automatic audit and
retrieval callbacks, matching the old source-write API. No `WithoutModelHooks`
acknowledgement is required. They retain the existing transaction/savepoint and
write-outcome reconciliation behavior. `DeleteUsing` applies soft deletion when
configured; `ForceDeleteUsing` requires a soft-delete declaration and preserves
the supplied destination visibility (`WithTrashed` includes deleted rows).

## Scoped keyed writes

Use package functions when a primary-key write must also retain an existing
selection, such as a tenant boundary:

```go
visible := Users.Query().Where(Users.TenantID.Eq(tenantID))
user, err := orm.Update(ctx, tx, visible, Users.Key(), id, Users.Name.Set("Ada"))
removed, err := orm.Delete(ctx, tx, visible, Users.Key(), id)
```

Both predicates constrain the write. The key must come from that exact table
declaration. Ordinary lifecycle hooks, observers, complete returning hydration,
savepoints and single-row cardinality checks remain active. A scope mismatch
returns `database.NotFound`. Delete uses soft deletion when declared; otherwise
it physically removes the row. Unknown commit outcomes retain `WriteError`
reconciliation candidates and must not be retried automatically.

## UUID seeks and bounded lifecycle writes

Use `UUIDLess`, `UUIDLessOrEqual`, `UUIDGreater` and `UUIDGreaterOrEqual` for
PostgreSQL bytewise UUID comparisons. Both the query model and `model.ID[T]` owner
remain checked by Go. Nullable UUID columns compare present values; SQL NULL
does not match. The corresponding `expr` functions accept scoped row expressions;
use their `Nullable` variants for nullable row values. They add no column methods.

```go
page := Files.Query().Where(orm.UUIDGreater(Files.Key(), after)).
    OrderBy(Files.Key().Asc()).Limit(100)
```

`UpdateEach(ctx, writer, query, limit, draft)` accepts a callback of type
`func(context.Context, *database.Tx, M) ([]orm.Assignment[M], error)`.
`DeleteEach(ctx, writer, query, limit)` performs normal physical or soft deletion.
`RestoreEach(ctx, writer, query, soft, limit)` restores only trashed candidates.
`ForceDeleteEach(ctx, writer, query, soft, limit)` physically deletes candidates,
preserving the query’s active/trashed visibility. Both require the declaration’s
`SoftDeletable[M]` token, such as `Users.Soft`; foreign or zero tokens fail before I/O.

All four operations lock the complete candidate set in primary-key order, require a positive
limit no greater than `MaxPerModelWriteRows`, and reject overflow and duplicate
identities before any draft or lifecycle callback. Incoming lock policies are
rejected; candidate selection always uses a waiting `FOR UPDATE`. Ordering, pagination,
eager-loading options and `WithoutModelHooks` are rejected.

Each row uses the normal hooks, observers, mutators, encryption, timestamps and
change tracking. Internal candidate hydration skips retrieval hooks. Rows are
closed before callbacks; propagate the supplied callback context and transaction
to nested work. One atomic transaction (or one enclosing savepoint on a supplied
transaction) covers all candidates, with no savepoint per model. Cancellation,
callback failures and later write failures roll back the operation and discard
its after-commit work. Successful results follow primary-key order.

An uncertain commit or committed after-commit failure returns no successful rows.
`*orm.WriteError[[]M]` retains reconciliation candidates; `Candidate()` is not
proof of persistence. There is no automatic retry. These bounded lifecycle
operations differ from set-based `UpdateAll`/`DeleteAll`, which explicitly skip
per-model behavior.

## Declaration roles

Declare managed timestamp pairs with `orm.CreatedAt`/`orm.CreatedAtOf` and
`orm.UpdatedAt`/`orm.UpdatedAtOf`. These columns retain ordinary time predicates,
ordering, projections and aggregates. A managed field declared through `TimeOf`
fails startup validation with the field name and the required constructor. A lone,
unmanaged creation timestamp keeps `TimeOf` and its existing database-default
behavior; `NoTimestamps` continues to disable conventional timestamp management.

Mutated fields use `Input*` declarations even when input and stored types match:
`orm.InputTextOf[User, string](b, &m.Name, nil)` discovers `MutateName`; passing a
transform function declares it explicitly. Ordinary columns with mutators fail
startup. Each Input capability family has a distinct private layout: converting a
column or enum into an ordered input field is rejected by the compiler.

`Table.Key()` is for lookup and ordinary key assignments. `NumberKey` and `TextKey`
preserve that role. Applications need `PrimaryOf` only when mapping a primary key
in insert-select: declare `ID orm.Primary[User, int64]` and bind it with
`orm.PrimaryOf(b, table.Key())`. Numeric/text primary adapters retain their role.
`PrimaryOf` cannot coexist with `InputKeyOf`; neither lookup keys nor mutated input
keys can be used as insert-select targets. All primary roles are excluded from
source-update targets. `KeyColumn` remains a read/relation interface and cannot
restore mapping eligibility after role erasure.

Expose soft-delete authority as an ordinary declaration field:

```go
type UserFields struct {
    orm.Table[User, int64]
    DeletedAt orm.SoftDelete[User, time.Time]
    Soft orm.SoftDeletable[User]
}
// Inside Define:
deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
fields := UserFields{table, deleted, orm.SoftDeletes(deleted)}
// After declaration:
_, err := orm.ForceDelete(ctx, db, Users.Query().WithTrashed(), Users.Soft, Users.Key(), id)
```

Every force-delete entry point requires this token, including `ForceDeleteAll`,
`rows.ForceDeleteUsing` and pivot `ForceDetach`. Tokens are bound to the exact
model declaration; zero and foreign-declaration tokens fail before SQL. Query
visibility remains unchanged. Normal soft deletion does not require the token.

`rows.MapInsert` and `rows.MapUpdate` accept concrete eligible column layouts.
Both reject Input fields and managed update timestamps; creation timestamps remain
eligible. Embedding a column in an application wrapper does not grant mapping
eligibility. Runtime declaration, ownership and SQL-mapping checks remain active.

### Framework adapters

Extension fields remain ordinary `translations.Text`, `attachments.One[M]`,
`attachments.Many[M]` and `metadata.Value[V]` values on the model. Bind each with
`orm.SlotOf(b, &m.Title)` inside `Define`; it adds no persisted column. Declare
policies separately with `translations.TextOf`, `attachments.OneOf` /
`ManyOf` and `metadata.ValueOf`, passing
`extensions.BindSlot(Articles.Key(), Articles.Title)`. The owner comes from
`extensions.DefineORMOwner("articles", orm.IdentityOf(Articles.Table,
Articles.Key()), extensions.OwnerOptions{StorageModel: "articles"})`. Keep that
storage identity pinned across table renames.

Bind each descriptor to its concrete manager once, then pass
`bound.Title.Relation(Articles.Title)` to `With`, `Load` or
`LoadMissing`. This uses the existing extension fetcher and shared relation row
budgets, including loaded-empty values and batch splitting. Register
`slots.DeclareORM(owner, parts)` through the existing model-extension
assembly to retain deletion cleanup. Runtime managers never live in the table.

HTTP binding accepts `modelbinding.ByKey(db, orm.Keyed(Users.Query(), Users.Key()),
pathKey)`. Use `ByColumn` for a unique alternate key, `ThroughRelation` for a direct
parent relationship, and `ThroughSelectedRelation` to retain earlier nested models.
Duplicate matches fail before eager loading; missing or out-of-scope matches keep
the existing 404 behavior. Binding does not acquire authorization or locks.

`factory.Define(Users.Table, build)` accepts a builder returning
`[]orm.Assignment[User]`. Existing states, relationship composition and after-create
hooks work with that concrete draft type. Batch creation uses `orm.InsertEach`,
which invokes every row's lifecycle in one bounded transaction, with one enclosing
savepoint for a caller-owned transaction. Unknown outcomes remain failures with
reconciliation candidates; no automatic retry occurs. Generated-model factory
and binding adapters remain available during the staged migration.

Pagination adapters consume `orm.PageResult`, `orm.SimplePage`, `orm.CursorRequest`
and `orm.CursorPage`. These and the legacy `query` names alias one set of shared SQL pagination
contracts in `internal/querycore`. Validation, field shapes, cursor bytes and
redacted formatting are unchanged; the reflected defining package is now the
shared owner. Applications continue using the public aliases. HTTP pagination
retains the published cursor schema identity, so manifests and generated clients
do not inherit the implementation package move.

The extension namespace uses `TextHandle`, `OneHandle`, `ManyHandle` and
`ValueHandle`: direct typed methods over private, non-generic callback state.
These handles reuse the existing managers and preserve publication outcomes;
they do not expose legacy `Field`, `Collection` or `Key` descriptor families.
`OneHandle` and `ManyHandle` remain distinct, non-convertible capabilities.
Lookup diagnostics (`Keyed`, `Unique`, `RelatedUnique`) redact scoped values under
ordinary, detailed and Go-syntax formatting.

## Diagnostic redaction

ORM and legacy query handles redact retained predicate literals, bound data values,
assignment inputs, numeric guard bounds, cursor tokens, plan contents and fixed
pivot bindings from `fmt` formatting and interpolating errors such as
`fmt.Errorf("query: %+v", q)`. This includes `%v`, `%+v` and `%#v` through private
and nested wrapper fields. Structural query metadata, including `LIMIT`/`OFFSET`,
identifiers and SQL shape, is outside this guarantee and may remain visible, even
when the compiler sends it as a parameter. Compiled statement formatting omits its
retained SQL text too, because explicit raw SQL can contain literals.

Explicit accessors such as `Statement.SQL()`, `Statement.Arguments()`,
`Cursor.Token()`, mutation input readers and `Plan.Root()`/`Plan.JSON()` still
return their inspection data. Formatting the returned data is an explicit
disclosure; the guarantee does not redact hydrated models or page items.

## Lifecycle terminals and query plans

`CreateEach(ctx, db, query, drafts)` runs one normal creation lifecycle per input,
atomically, in input order. A valid empty batch does not acquire a transaction.
`CreateOrFirst(ctx, db, query, assignments...)` tries insertion first in a
savepoint. Only a unique violation from that INSERT on the model's own table
allows recovery: after rollback, it reads the first matching competitor. Recovery
requires server-reported schema, table and constraint metadata matching the
resolved destination; missing metadata preserves the violation. Hooks,
observers, hydration errors and uncertain outcomes never trigger recovery or
retry. The query must select the conflicting row; snapshot isolation can hide a
competitor, in which case the violation is returned. Internal reads skip Retrieved.
`FirstOrCreate` locks the first primary-ordered match, while `UpdateOrCreate`
accepts separate creation assignments and a typed update callback. Only the chosen
branch prepares assignments. Omitted creation fields inherit unambiguous top-level
equality predicates, including global scopes; mutated fields require explicit input.
A created row must satisfy the original query. Concurrent absent rows rely on
physical unique constraints; these functions do not retry. Internal model reads
skip retrieval callbacks. Read ordering, windows and eager loads are rejected.

`PruneBatch(ctx, db, query, size, mass)` physically removes a bounded batch of
ordinary models. Soft-delete models use `ForcePruneBatch(ctx, db, query, soft,
size, mass)` with their declaration token. Selection locks rows in primary-key
order and preserves filters and visibility. With `mass=false`, every model runs
its deletion lifecycle. `mass=true` explicitly acknowledges one DELETE without
per-model hooks, observers or audit. Each batch is atomic. `prune.ORMModel` and
`prune.ORMSoftModel` register these operations with the [bounded prune registry](../prune/README.md).
The registry uses `orm.MaxPerModelWriteRows`; generated-query `prune.Model`
remains a compatibility adapter during retirement.

`Explain` returns a bounded typed PostgreSQL `Plan` without running the SELECT;
`ExplainAnalyze` explicitly executes it. Neither hydrates models, dispatches
retrieval callbacks nor runs eager loads. Locked models use `ExplainLocked` or
`ExplainAnalyzeLocked`, requiring `*database.Tx`. The corresponding `rows`
functions accept ordinary `Result`, locked results or transaction-owned records
and scalars. Adapt projections, scalars and sets through the existing `rows.Read*`
functions. `rows.ExplainCursor`/`ExplainValueCursor` and their Analyze variants
plan canonical cursor ordering without adding a page boundary. Plan formatting is
redacted; explicit `Root` and `JSON` accessors return
owned snapshots and may expose server predicates and bound values.

Numeric adjustments retain `Number.Increment`/`Decrement` and nullable equivalents.
Amounts are positive and finite; `AtMost` guards increments and `AtLeast` guards
decrements. Numeric primary and lookup-key adapters expose comparisons, ordering
and identity assignment, but no adjustment methods. Managed timestamps and input
columns also have no numeric adjustment capability. A guarded per-key update
returning no row reports `ErrBoundExceeded` (including missing keys).


Projection callbacks use `rows.Each(ctx, db, report, consume)`. They receive one
fully decoded value at a time while rows remain open. Callback errors stop further
decoding and close the result; values already consumed remain consumed if a later
decode or close fails. Use `report.All` when the caller needs an all-or-error slice.

## Advisory validation lookups

`validation/database` accepts an ORM model query and one typed stored column:

```go
available := databasevalidation.Unique(db, Users.Query(), Users.Email)
active := databasevalidation.Exists(db, Users.Query().Where(Users.Status.Eq(StatusActive)), Users.Key())
```

`orm.Lookup(Users.Query(), Users.Email)` is the underlying observation handle.
It preserves filters, global scopes, soft-delete visibility and stored codecs,
without hydrating models, retrieval hooks or implicit transactions. Nullable and
mutated columns accept their non-null stored value type. Batch observations use
SQL equality in groups of 64; empty lists perform no I/O. Ordered, paginated,
eager-loaded and locked sources cannot be used as complete lookup scopes.

`databasevalidation.Scoped` derives a scope at check time;
`databasevalidation.UniqueIgnoring` derives a typed key exclusion from a supplied
`validation.Slot`. Keep tenant predicates and derive exclusions from trusted route
or actor data. Errors never prove availability. These reads are advisory: database
constraints still govern writes. See [validation](../../docs/guides/validation.md#advisory-database-rules)
for composition and safe metadata exports.

### Shared extension loading

Declaration-based attachment, translation and metadata handles use the shared
external-slot loader in `internal/extensionslot`. Public handles retain concrete
owner, key and value types through direct method shells over private callback
state. Loading keeps per-slot parent batches, loaded-empty values, separate fetched
and attached budgets, and the caller's executor. Failed fetches do not attach an
incomplete batch tree.

`FoundryExtension` and the legacy query adapter require a validated query-family
capability. They accept concrete framework slot state; capabilities are never
passed to slot accessors or application callbacks. Legacy generated slot and owner
entry points remain available during consumer migration. Their predicate conversion
happens only at those compatibility entry points; ORM owners retain ORM identity
and scope declarations.

Bounded Explain execution and plan decoding also share querycore. The public
`orm.Plan`/`PlanNode` and legacy aliases retain their methods and data contract;
their defining Go package is now the internal shared owner.
