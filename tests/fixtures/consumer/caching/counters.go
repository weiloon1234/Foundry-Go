package caching

import (
	"context"
	"time"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

// ProfileViews is an evictable, approximate activity count, not durable accounting.
// The first update sets a one-day TTL; later updates preserve the same expiry.
var ProfileViews = cache.DefineCounter("profile-views-v1", cache.TextKeys[model.ID[mutatorqueries.Member]]())

type ViewCounts = cache.Counter[model.ID[mutatorqueries.Member]]

func BindViewCounts(store *cache.Store) (ViewCounts, error) { return ProfileViews.Bind(store) }
func RecordProfileView(ctx context.Context, counts ViewCounts, id model.ID[mutatorqueries.Member]) (int64, error) {
	return counts.Increment(ctx, id, 1, cache.For(24*time.Hour))
}
func UndoProfileView(ctx context.Context, counts ViewCounts, id model.ID[mutatorqueries.Member]) (int64, error) {
	return counts.Decrement(ctx, id, 1, cache.For(24*time.Hour))
}
