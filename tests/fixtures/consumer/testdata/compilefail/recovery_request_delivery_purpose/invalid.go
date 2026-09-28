package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = challenge.RequestCallbacks[recovering.Member, model.ID[recovering.Member], string, challenge.PasswordReset]{Deliver: func(context.Context, emailverification.Issued[recovering.Member]) error { return nil }}
