package terminal_prune

import (
	"context"
	m "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Run(ctx context.Context, db database.Transactor, mass bool) (int64, error) {
	return orm.ForcePruneBatch(ctx, db, m.Members.Query().OnlyTrashed(), m.Members.Soft, 100, mass)
}
