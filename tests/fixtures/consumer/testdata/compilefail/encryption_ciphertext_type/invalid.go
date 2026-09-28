package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func wrong(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, plain secret.String) {
	_, _ = keys.Decrypt(ctx, owner, plain)
}
