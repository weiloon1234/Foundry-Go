package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func decodePlan(ctx context.Context, data []byte, analyze bool) (Plan, error) {
	return querycore.DecodePlan(ctx, data, analyze)
}
