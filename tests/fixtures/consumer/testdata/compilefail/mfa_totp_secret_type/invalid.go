package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/encryption"
)

func wrong(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, code mfa.TOTPCode) {
	_, _ = multifactor.EncryptEnrollment(ctx, keys, owner, code)
}
