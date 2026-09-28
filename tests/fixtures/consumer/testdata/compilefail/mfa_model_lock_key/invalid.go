package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = mfa.Model[multifactor.Account, model.ID[multifactor.Account]]{Lock: func(context.Context, *database.Tx, string) (value.Optional[multifactor.Account], error) {
	return value.Optional[multifactor.Account]{}, nil
}}
