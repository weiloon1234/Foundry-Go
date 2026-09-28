package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth/password"
)

func bad(ctx context.Context, hasher *password.Hasher) { hasher.Hash(ctx, "raw password") }
