package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth/password"
)

func bad(ctx context.Context, hasher *password.Hasher, hash password.Hash) { hasher.Hash(ctx, hash) }
