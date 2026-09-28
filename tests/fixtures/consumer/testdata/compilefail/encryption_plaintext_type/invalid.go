package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/encryption"
)

func wrong(ctx context.Context, keys *encryption.Keyring, owner encryption.Context) {
	_, _ = keys.Encrypt(ctx, owner, "raw string")
}
