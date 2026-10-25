package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type account struct {
	ID    int
	Email string
}
type label struct{ ID int }
type fields struct {
	orm.Table[account, int]
	Email orm.Text[account, string]
}

var accounts = orm.Define("accounts", func(b orm.Builder[account], m *account) fields {
	return fields{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Email)}
})

func invalid(ctx context.Context, db *database.DB, r *audit.Recorder, id audit.ModelID[label]) {
	identity := orm.IdentityOf(accounts.Table, accounts.Key())
	_, _ = audit.FindModel(ctx, db, r, identity.Reference(1), id)
}
