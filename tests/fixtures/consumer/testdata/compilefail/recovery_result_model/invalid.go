package invalid

import (
	"context"
	"foundry.test/consumer/passwords"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
)

func bad(ctx context.Context, reset *recovering.Reset, token passwordreset.Token[recovering.Member], plain password.Plaintext) {
	var account passwords.Account
	var err error
	account, err = reset.Complete(ctx, token, plain)
	_, _ = account, err
}
