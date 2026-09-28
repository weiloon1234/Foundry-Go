package invalid

import (
	"context"
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

func invalid(ctx context.Context, db *database.DB, producer *events.Outbox) {
	_, _ = eventqueries.Created.Find(ctx, db, producer, outbox.ID[string]{})
}
