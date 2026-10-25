package orm

import (
	"context"
	"database/sql/driver"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryRecord supplies complete model metadata through the validated metadata family.
// Example: FoundryRecord[Order](scope,sqlowner.Metadata()).
func FoundryRecord[M any](scope ormcore.Scope, seal sqlowner.MetadataSeal) ormcore.Record {
	if !seal.Valid() {
		return ormcore.Record{Err: invalid("invalid SQL owner capability")}
	}
	table, ok := scope.Owner.(*tableCore)
	if !ok || table == nil || table.typ != reflect.TypeFor[M]() {
		return ormcore.Record{Err: invalid("record requires its model declaration")}
	}
	return modelRecord(table)
}

func modelRecord(table *tableCore) ormcore.Record {
	return ormcore.Record{Primary: table.model.Primary, Fields: func() []ormcore.RecordField { return modelRecordFields(table) }, Type: table.typ, Owner: table, Columns: slices.Clone(table.model.Columns), Err: declarationCoreError(table), Checks: []func() error{func() error { return tableError(table) }}, Scan: func(row database.Row, p unsafe.Pointer) error { return hydrate(table, row, p) }, Callbacks: func(ex database.Executor, data func() (unsafe.Pointer, int)) *ormcore.ReadCallbacks {
		batch := &retrievalBatch{table: table}
		callbacks := readBatches(ex, batch)
		complete := callbacks.Complete
		callbacks.Complete = func(ctx context.Context, set lifecycle.Observers) error {
			base, count := data()
			batch.capture(base, count)
			return complete(ctx, set)
		}
		return callbacks
	}}
}

// FoundryLockedRecord retains the model's lock inside a transaction-only record.
// Example: FoundryLockedRecord(Orders.Query().ForUpdate(),sqlowner.Metadata()).
func FoundryLockedRecord[M any](q LockedQuery[M], seal sqlowner.MetadataSeal) ormcore.RecordSource {
	if !seal.Valid() {
		return ormcore.RecordSource{Source: ormcore.Source{Err: invalid("invalid SQL owner capability")}}
	}
	source := FoundrySource(Query[M]{state: q.state}, seal)
	if source.Err != nil {
		return ormcore.RecordSource{Source: source, Transaction: true}
	}
	source.Node.Locks = []querycore.LockSpec{{Strength: q.state.lock, Behavior: q.state.behavior}}
	if q.state.lock == 0 {
		source.Err = invalid("locked record requires a lock clause")
	}
	return ormcore.RecordSource{Source: source, Record: modelRecord(q.state.table), Transaction: true}
}

func modelRecordFields(table *tableCore) []ormcore.RecordField {
	fields := make([]ormcore.RecordField, len(table.columns))
	for i, c := range table.columns {
		fields[i] = ormcore.RecordField{Name: c.metadata.Name, Sensitive: c.sensitive, Get: c.get, Decode: func(raw any) (driver.Value, error) {
			v, err := c.automatic(raw)
			if err != nil {
				return nil, err
			}
			return v.Bind()
		}}
	}
	return fields
}
