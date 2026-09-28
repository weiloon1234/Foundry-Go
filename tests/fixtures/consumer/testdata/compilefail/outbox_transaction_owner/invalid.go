package invalid

import (
	"context"
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
)

func invalid(ctx context.Context, db *database.DB, producer *events.Outbox) {
	_, _ = eventqueries.Created.Enqueue(ctx, db, producer, eventqueries.RecordCreated{})
}
