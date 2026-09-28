package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func wrong(ctx context.Context, sessions *multifactor.Sessions, pending secret.String, factor auth.SecondFactor[recovering.Member, model.ID[recovering.Member]]) {
	_, _ = sessions.CompleteMFA(ctx, pending, factor, session.IssueOptions{})
}
