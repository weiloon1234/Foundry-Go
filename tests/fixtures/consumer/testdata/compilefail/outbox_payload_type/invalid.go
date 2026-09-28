package invalid

import (
	"context"
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
)

func invalid(ctx context.Context, tx *database.Tx, producer *events.Outbox) {
	_, _ = eventqueries.Created.Enqueue(ctx, tx, producer, "wrong")
}
