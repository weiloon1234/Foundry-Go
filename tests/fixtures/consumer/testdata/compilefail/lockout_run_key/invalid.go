package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
)

type Email string

func bad(ctx context.Context, th lockout.Throttle[Email], key int) {
	th.Run(ctx, key, func(context.Context) (bool, error) { return true, nil })
}
