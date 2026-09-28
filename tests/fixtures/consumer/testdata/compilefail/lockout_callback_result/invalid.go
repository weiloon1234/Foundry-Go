package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
)

func bad(ctx context.Context, th lockout.Throttle[string]) {
	th.Run(ctx, "member", func(context.Context) (string, error) { return "yes", nil })
}
