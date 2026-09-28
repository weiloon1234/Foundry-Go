package invalid

import (
	"context"
	"foundry.test/consumer/bootstrap"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

func invalid(bound jobs.Bound[bootstrap.Profile]) {
	_, _ = bound.Dispatch(context.Background(), bootstrap.Operator{}, jobs.Options[bootstrap.Profile]{})
}
