package orm

import (
	"context"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryModelRead retains complete hydration, retrieval and eager-load ownership.
// Example: FoundryModelRead(Users.Query().With(UserRelations.Orders),sqlowner.Metadata()).
func FoundryModelRead[M any](q Query[M], seal sqlowner.MetadataSeal) ormcore.RecordSource {
	if !seal.Valid() {
		return ormcore.RecordSource{Source: ormcore.Source{Err: invalid("invalid SQL owner capability")}}
	}
	plain := q
	plain.state.relations = nil
	source := FoundrySource(plain, seal)
	if source.Err != nil {
		return ormcore.RecordSource{Source: source}
	}
	source.Checks = []func() error{sourceCheck(q.state)}
	record := modelRecord(q.state.table)
	if len(q.state.relations) != 0 {
		callbacks := record.Callbacks
		record.Callbacks = func(ex database.Executor, data func() (unsafe.Pointer, int)) *ormcore.ReadCallbacks {
			hooks := callbacks(ex, data)
			return &ormcore.ReadCallbacks{Needed: func(lifecycle.Observers) bool { return true }, Complete: func(ctx context.Context, set lifecycle.Observers) error {
				if err := hooks.Complete(ctx, set); err != nil {
					return err
				}
				base, count := data()
				return loadPlans(ctx, ex, base, count, q.state.relations)
			}}
		}
	}
	return ormcore.RecordSource{Source: source, Record: record}
}
