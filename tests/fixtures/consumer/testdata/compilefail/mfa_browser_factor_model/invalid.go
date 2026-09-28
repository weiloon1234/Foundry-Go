package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(ctx context.Context, browser *multifactor.Browser, factor auth.SecondFactor[recovering.Member, model.ID[recovering.Member]]) {
	_, _ = browser.CompleteMFA(ctx, factor, session.IssueOptions{})
}
