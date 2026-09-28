package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"github.com/weiloon1234/Foundry-Go/auth/session"
)

func bad(ctx context.Context, sessions *authenticating.UserSessions, hash session.Digest) {
	_, _ = sessions.Rotate(ctx, hash)
}
