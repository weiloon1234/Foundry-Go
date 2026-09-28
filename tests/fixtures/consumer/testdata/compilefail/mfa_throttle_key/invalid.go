package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/database"
)

func wrong(store *mfa.Store, provider multifactor.Provider, throttle lockout.Throttle[string]) {
	_, _ = multifactor.NewFactors(store, provider, throttle, func(context.Context, *database.Tx, multifactor.Account) error { return nil })
}
