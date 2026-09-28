package invalid

import (
	"context"
	"foundry.test/consumer/passwords"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(ctx context.Context, tx *database.Tx, provider recovering.Provider, result auth.PasswordResult[passwords.Account, model.ID[passwords.Account]]) {
	_, _ = provider.RecheckPassword(ctx, tx, result)
}
