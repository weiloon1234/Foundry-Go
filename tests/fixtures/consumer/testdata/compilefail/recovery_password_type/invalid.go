package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
)

func bad(ctx context.Context, reset *recovering.Reset, token passwordreset.Token[recovering.Member], hash password.Hash) {
	reset.Complete(ctx, token, hash)
}
