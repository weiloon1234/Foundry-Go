package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func bad(ctx context.Context, tokens *authenticating.UserTokens, request foundryhttp.RefreshTokenRequest) {
	tokens.Refresh(ctx, request.RefreshToken)
}
