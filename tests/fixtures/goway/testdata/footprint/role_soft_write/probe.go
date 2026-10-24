package role_soft_write

import (
	"context"
	m "foundry.test/goway/testdata/footprint/role_soft_read"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Write(ctx context.Context, db database.Transactor, id int64) (m.Model, error) {
	return orm.ForceDelete(ctx, db, m.Models.Query().WithTrashed(), m.Models.Soft, m.Models.Key(), id)
}
