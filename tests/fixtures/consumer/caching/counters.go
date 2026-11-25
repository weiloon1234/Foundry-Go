package caching

import (
	"context"
	"time"

	models "foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// ProfileViews is an evictable, approximate activity count, not durable accounting.
// The first update sets a one-day TTL; later updates preserve the same expiry.
// doc-example:begin caching-1
var ProfileViews = cache.DefineCounter("profile-views-v1", keyspace.TextKeys[model.ID[models.Member]]())

// doc-example:end caching-1

type ViewCounts = cache.Counter[model.ID[models.Member]]

func BindViewCounts(store *cache.Store) (ViewCounts, error) { return ProfileViews.Bind(store) }
func RecordProfileView(ctx context.Context, counts ViewCounts, id model.ID[models.Member]) (int64, error) {
	return counts.Increment(ctx, id, 1, cache.For(24*time.Hour))
}
func UndoProfileView(ctx context.Context, counts ViewCounts, id model.ID[models.Member]) (int64, error) {
	return counts.Decrement(ctx, id, 1, cache.For(24*time.Hour))
}
