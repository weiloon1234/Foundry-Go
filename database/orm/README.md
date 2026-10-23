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
`SoftDeleteOf`. `Table.ForceDelete(key)` physically deletes active or trashed
records; `Query.ForceDeleteAll` respects the query's visibility. Explicit timestamp roles
win over names; explicit timestamp constructors conflict with `NoTimestamps()`.
`CreatedAt`, `UpdatedAt`, and `SoftDelete` retain the instant type as their second
type parameter. Upsert and cursor pagination remain deferred.

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
