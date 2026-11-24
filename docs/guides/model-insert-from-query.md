# Insert models from a typed query

`rows.InsertFrom(source, destination, mappings...)` builds one set-based insertion. Each `rows.MapInsert` retains source scope, destination model, exact stored type and nullability. Fixed inputs use ordinary typed assignments.

Declare the destination with an exposed primary insertion handle. Map stored
columns from the source; supply the mutated name through a typed input.

<!-- compiled-source: tests/fixtures/goway/modelguide/archive.go -->
```go
package modelguide

import (
	"context"
	"strings"
	"time"

	"foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

//foundry:model table=guide_member_archives var=Archives key=ID
type ArchivedMember struct {
	ID        int64  `foundry:"member=ID,key-role=primary"`
	Name      string `foundry:"input=string"`
	Body      []byte
	CreatedAt time.Time `foundry:"role=created-at"`
	UpdatedAt time.Time `foundry:"role=updated-at"`
}

func (ArchivedMember) MutateName(name string) (string, error) {
	return strings.TrimSpace(name), nil
}

func CopyMembers(ctx context.Context, tx *database.Tx) (int64, error) {
	source, destination := hooks.Members, Archives
	return rows.InsertFrom(rows.From(source.Query()), destination.Query(),
		rows.MapInsert(destination.ID, expr.Value(source.Key())),
		rows.MapInsert(destination.Body, expr.Value(source.Body)),
		rows.MapInsert(destination.CreatedAt, expr.Value(source.CreatedAt))).
		Values(orm.Set(destination.Name, "copy")).Exec(ctx, tx)
}
```

The destination is a separate archive table; its key retains the source identity. Migrations must create both tables. A repeated import can conflict with an existing archive key. The [archive consumer](../../tests/fixtures/consumer/linkqueries/insert_select_postgres_test.go) exercises source insertion against PostgreSQL.

## Select stored values and supply inputs

SQL mappings append. `Values(assignments...)` replaces the fixed inputs for every selected row. Repeated mappings or a column present in both mappings and fixed values fail validation. Nullable expressions cannot populate required destinations; explicit nullable widening is available through the expression API.

`MapInsert` accepts only `orm.InsertableRole` column handles. Go-mutated fields require fixed `Values` inputs and cannot accept SQL mappings. Managed update time is also excluded. Mapping an unmutated primary requires an exposed key with `key-role=primary`; a lookup key alone does not grant this permission. Complete [typed roles](model-declarations.md) enforce these restrictions directly on `orm.ColumnHandle`.

SQL mappings do not call Go accessors or mutators. Fixed inputs transform once per attempt inside the transaction. Omitted input remains omitted, NULL skips scalar mutators, and supplied zero remains a value. Use bounded `CreateEach` when every source row needs Go transformations or lifecycle callbacks.

## Timestamps, defaults and results

Managed creation time is supplied when neither mapped nor assigned. Managed update time comes from the same operation clock sample. Omitted nullable/defaulted fields remain PostgreSQL-owned. Required fields without defaults must be supplied.

Insert-select does not synthesize one Go UUID for every selected row. Supply a primary mapping or a real migration-owned default. A fixed key in `Values` is one fixed key and can conflict across rows. A plan with no supplied columns is invalid.

`Exec` returns an affected count without collecting models and processes the complete source window. `Returning(ctx, writer, limit)` collects at most 1,000 complete models, within the explicit positive bound. Excess rows roll back the insertion; they never truncate success or alter the source's window. The bound limits retained models, not database work or individual field size. Return order does not map to source order.

## Transactions and lifecycle

Source predicates, ordering, windows, scopes and soft-delete visibility remain effective. The insertion is explicitly set-based and skips per-model hooks, observers, audit and retrieval; no separate hook acknowledgement is required. Literal inputs are validated even when no source rows match.

An operation owns a transaction or nested savepoint. Invalid inputs, constraints, cancellation or excess returned rows roll it back without hidden retries. Uncertain commit results retain reconciliation data in `orm.WriteError[int64]` for `Exec` or `orm.WriteError[[]M]` for `Returning`. Ordinary plan formatting omits inputs.
