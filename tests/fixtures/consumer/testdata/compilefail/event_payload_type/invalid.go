package invalid

import (
	"context"
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/events"
)

func invalid(ctx context.Context, bus *events.Bus) { _ = eventqueries.Created.Dispatch(ctx, bus, 7) }
