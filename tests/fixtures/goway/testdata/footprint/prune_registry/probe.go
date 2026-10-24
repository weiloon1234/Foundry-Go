package prune_registry

import (
	"context"
	m "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/prune"
)

func Run(ctx context.Context, db database.Transactor) (prune.Result, error) {
	r, err := prune.New(prune.ORMSoftModel("app.members", m.Members.Query().OnlyTrashed(), m.Members.Soft, prune.Lifecycle))
	if err != nil {
		return prune.Result{}, err
	}
	return r.Run(ctx, db, prune.DefaultOptions())
}
