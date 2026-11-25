package modelguide

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin account-provider

//foundry:model table=guide_accounts var=Accounts key=ID
type Account struct {
	ID      int64
	Enabled bool
}

func AccountProvider(db database.Executor) auth.Provider[Account, int64] {
	return auth.DefineProvider("accounts", (Account{}).FoundryReference(),
		func(ctx context.Context, id int64) (value.Optional[Account], error) {
			return orm.Find(ctx, db, Accounts.Table, id)
		}, func(_ context.Context, account Account) (bool, error) { return account.Enabled, nil })
}

// doc-example:end account-provider
