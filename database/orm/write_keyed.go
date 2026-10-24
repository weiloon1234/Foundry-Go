package orm

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Update patches one primary key within the query's current visibility and hydrates
// its complete returned model. Scope predicates, lifecycle and cardinality checks
// remain active. Example: orm.Update(ctx, db, visible, Users.Key(), id, Users.Name.Set("Ada")).
func Update[M any, K comparable](ctx context.Context, writer database.Transactor, q Query[M], key Key[M, K], id K, assignments ...Assignment[M]) (M, error) {
	if q.state.table == nil || key.key.core == nil || key.key.core != q.state.table.primary {
		return *new(M), invalid("update requires this table's primary key")
	}
	return writeOne[M](ctx, writer, q.Where(key.Eq(id)).state, querycore.UpdateModel, assignments)
}

// Delete removes one primary key within the query's current visibility and returns
// its complete model. Declared soft deletion retains the physical row; otherwise
// deletion is physical. Example: orm.Delete(ctx, db, visible, Users.Key(), id).
func Delete[M any, K comparable](ctx context.Context, writer database.Transactor, q Query[M], key Key[M, K], id K) (M, error) {
	if q.state.table == nil || key.key.core == nil || key.key.core != q.state.table.primary {
		return *new(M), invalid("delete requires this table's primary key")
	}
	return writeOne[M](ctx, writer, q.Where(key.Eq(id)).state, querycore.DeleteModel, nil)
}
