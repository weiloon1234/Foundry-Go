package invalid

import (
	"context"
	"foundry.test/consumer/passwords"
	"foundry.test/consumer/recovering"
)

func bad(ctx context.Context, reset *recovering.Reset, account passwords.Account) {
	reset.Issue(ctx, account.FoundryReference())
}
