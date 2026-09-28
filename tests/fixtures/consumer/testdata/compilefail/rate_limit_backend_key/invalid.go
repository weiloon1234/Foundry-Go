package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

func wrong(b ratelimit.Backend, key lease.Key) {
	_, _ = b.RateLimit(context.Background(), key, ratelimit.PerSecond(1), 1)
}
