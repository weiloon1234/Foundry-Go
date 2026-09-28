package invalid

import (
	"context"
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
)

func invalid(ctx context.Context, db *database.DB, bus *events.Bus) {
	_ = eventqueries.Created.AfterCommit(ctx, db, bus, eventqueries.RecordCreated{})
}
