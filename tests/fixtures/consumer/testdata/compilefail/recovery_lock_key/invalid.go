package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = passwordreset.Model[recovering.Member, model.ID[recovering.Member]]{Lock: func(context.Context, *database.Tx, string) (value.Optional[recovering.Member], error) {
	return value.Optional[recovering.Member]{}, nil
}}
