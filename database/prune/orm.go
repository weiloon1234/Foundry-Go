package prune

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// ORMModel registers an ordinary ORM model for bounded lifecycle or mass pruning.
// Example: prune.ORMModel("app.logs", Logs.Query(), prune.Lifecycle).
func ORMModel[M any](name Name, selection orm.Query[M], mode Mode) Target {
	return Target{name: name, mode: mode, run: func(ctx context.Context, db database.Transactor, size int) (int64, error) {
		return orm.PruneBatch(ctx, db, selection, size, mode == Mass)
	}}
}

// ORMSoftModel registers physical pruning with the exact soft-model declaration token.
// Example: prune.ORMSoftModel("app.users", Users.Query().OnlyTrashed(), Users.Soft, prune.Mass).
func ORMSoftModel[M any](name Name, selection orm.Query[M], soft orm.SoftDeletable[M], mode Mode) Target {
	return Target{name: name, mode: mode, run: func(ctx context.Context, db database.Transactor, size int) (int64, error) {
		return orm.ForcePruneBatch(ctx, db, selection, soft, size, mode == Mass)
	}}
}
