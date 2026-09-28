package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
)

func bad(ctx context.Context, tokens *authenticating.UserTokens, raw string) {
	_, _ = tokens.Refresh(ctx, raw)
}
