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

Hooks, mutators, JSON and encryption remain outside this increment. Direct
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
