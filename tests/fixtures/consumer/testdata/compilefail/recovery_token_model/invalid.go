package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
)

func bad(ctx context.Context, reset *recovering.Reset, other passwordreset.Token[struct{}], plain password.Plaintext) {
	reset.Complete(ctx, other, plain)
}
