# Insert models from a typed query

`rows.InsertFrom(source, destination, mappings...)` builds one set-based insertion. Each `rows.MapInsert` retains source scope, destination model, exact stored type and nullability. Fixed inputs use ordinary typed assignments.

<!-- orm-example: source-insert -->
```go
type ArchivedMember struct {
	ID                   int64
	Name                 string
	Body                 []byte
	CreatedAt, UpdatedAt time.Time
}

func (ArchivedMember) MutateName(name string) (string, error) {
	return strings.TrimSpace(name), nil
}

type ArchiveTable struct {
	orm.Table[ArchivedMember, int64]
	ID        orm.Primary[ArchivedMember, int64]
	Name      orm.InputText[ArchivedMember, string, string]
	Body      orm.Column[ArchivedMember, []byte]
	CreatedAt orm.CreatedAt[ArchivedMember, time.Time]
	UpdatedAt orm.UpdatedAt[ArchivedMember, time.Time]
}

var Archives = orm.Define("member_archives", func(b orm.Builder[ArchivedMember], m *ArchivedMember) ArchiveTable {
	table := orm.TableOf(b, &m.ID)
	return ArchiveTable{table, orm.PrimaryOf(b, table.Key()),
		orm.InputTextOf[ArchivedMember, string](b, &m.Name, nil), orm.BytesOf(b, &m.Body),
		orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

func copyMembers(ctx context.Context, tx *database.Tx) (int64, error) {
	source, destination := hooks.Members, Archives
	return rows.InsertFrom(rows.From(source.Query()), destination.Query(),
		rows.MapInsert(orm.KeyExpr(destination.ID), expr.Value(orm.KeyExpr(source.Key()))),
		rows.MapInsert(orm.ColumnExpr(destination.Body), expr.Value(orm.ColumnExpr(source.Body))),
		rows.MapInsert(orm.TimeExpr(destination.CreatedAt), expr.Value(orm.TimeExpr(source.CreatedAt)))).
		Values(orm.InputTextSet(destination.Name, "copy")).Exec(ctx, tx)
}
```

The destination is a separate archive table; its key retains the source identity. Migrations must create both tables. A repeated import can conflict with an existing archive key. The [archive consumer](../../tests/fixtures/consumer/linkqueries/insert_select_postgres_test.go) exercises source insertion against PostgreSQL.

## Select stored values and supply inputs

SQL mappings append. `Values(assignments...)` replaces the fixed inputs for every selected row. Repeated mappings or a column present in both mappings and fixed values fail validation. Nullable expressions cannot populate required destinations; explicit nullable widening is available through the expression API.

`MapInsert` accepts only `orm.InsertTarget` columns. Go-mutated fields require fixed `Values` inputs and cannot accept SQL mappings. Managed update time is also excluded. Mapping an unmutated primary requires `PrimaryOf`; a lookup key alone does not grant this permission. Complete [typed roles](model-declarations.md) retain these restrictions through adapters.

SQL mappings do not call Go accessors or mutators. Fixed inputs transform once per attempt inside the transaction. Omitted input remains omitted, NULL skips scalar mutators, and supplied zero remains a value. Use bounded `CreateEach` when every source row needs Go transformations or lifecycle callbacks.

## Timestamps, defaults and results

Managed creation time is supplied when neither mapped nor assigned. Managed update time comes from the same operation clock sample. Omitted nullable/defaulted fields remain PostgreSQL-owned. Required fields without defaults must be supplied.

Insert-select does not synthesize one Go UUID for every selected row. Supply a primary mapping or a real migration-owned default. A fixed key in `Values` is one fixed key and can conflict across rows. A plan with no supplied columns is invalid.

`Exec` returns an affected count without collecting models and processes the complete source window. `Returning(ctx, writer, limit)` collects at most 1,000 complete models, within the explicit positive bound. Excess rows roll back the insertion; they never truncate success or alter the source's window. The bound limits retained models, not database work or individual field size. Return order does not map to source order.

## Transactions and lifecycle

Source predicates, ordering, windows, scopes and soft-delete visibility remain effective. The insertion is explicitly set-based and skips per-model hooks, observers, audit and retrieval; no separate hook acknowledgement is required. Literal inputs are validated even when no source rows match.

An operation owns a transaction or nested savepoint. Invalid inputs, constraints, cancellation or excess returned rows roll it back without hidden retries. Uncertain commit results retain reconciliation data in `orm.WriteError[int64]` for `Exec` or `orm.WriteError[[]M]` for `Returning`. Ordinary plan formatting omits inputs.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
