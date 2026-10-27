package terminal_explain

import (
	"context"
	m "foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Plan(ctx context.Context, db database.Executor) (orm.Plan, error) {
	return orm.Explain(ctx, db, m.Users.Query())
}
func Locked(ctx context.Context, tx *database.Tx) (orm.Plan, error) {
	return rows.ExplainAnalyzeTransaction(ctx, tx, rows.ForUpdate(rows.Read(rows.FromModel(m.Users.Query()))))
}
