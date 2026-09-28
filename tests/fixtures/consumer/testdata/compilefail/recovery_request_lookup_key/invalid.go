package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = challenge.RequestCallbacks[recovering.Member, model.ID[recovering.Member], string, challenge.PasswordReset]{Lookup: func(context.Context, string) (value.Optional[model.Reference[recovering.Member, string]], error) {
	return value.Optional[model.Reference[recovering.Member, string]]{}, nil
}}
