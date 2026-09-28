package invalid

import (
	"context"
	"foundry.test/consumer/passwords"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = auth.PasswordModel[recovering.Member, string]{Lock: func(context.Context, *database.Tx, recovering.Member) (value.Optional[passwords.Account], error) {
	return value.Optional[passwords.Account]{}, nil
}}
