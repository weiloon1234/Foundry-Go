package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/password"
)

func bad(ctx context.Context, reset *recovering.Reset, other emailverification.Token[recovering.Member], plain password.Plaintext) {
	reset.Complete(ctx, other, plain)
}
