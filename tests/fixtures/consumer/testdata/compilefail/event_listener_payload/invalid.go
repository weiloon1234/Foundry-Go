package invalid

import (
	"context"
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/events"
)

func invalid() {
	_, _ = eventqueries.Created.Declare(events.Listen("wrong", func(context.Context, int) error { return nil }))
}
