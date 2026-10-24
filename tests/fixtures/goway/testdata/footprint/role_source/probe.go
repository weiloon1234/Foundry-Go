package role_source

import (
	"context"
	m "foundry.test/goway/testdata/footprint/role_time_read"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Plan() orm.InsertSelect[m.Model, m.Model] {
	f := m.Models
	return rows.InsertFrom(rows.From(f.Query()), f.Query(), rows.MapInsert(f.ID, expr.Value(f.ID)), rows.MapInsert(f.At, expr.Value(f.At)), rows.MapInsert(f.CreatedAt, expr.Value(f.CreatedAt)))
}
func Write(ctx context.Context, db database.Transactor) (int64, error) { return Plan().Exec(ctx, db) }
