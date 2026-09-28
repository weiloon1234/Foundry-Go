package invalid

import (
	"context"
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/auth/password"
)

func bad(ctx context.Context, login *passwords.Login, plain password.Plaintext) {
	login.Authenticate(ctx, 42, plain)
}
