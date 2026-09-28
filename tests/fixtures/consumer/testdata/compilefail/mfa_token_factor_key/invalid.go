package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func wrong(ctx context.Context, tokens *multifactor.Tokens, pending secret.String, factor auth.SecondFactor[multifactor.Account, string]) {
	_, _ = tokens.CompleteMFA(ctx, pending, factor, token.IssueOptions[multifactor.Account]{})
}
