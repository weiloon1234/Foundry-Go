package caching

import (
	"context"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

func ProfileExists(ctx context.Context, profiles Profiles, id model.ID[mutatorqueries.Member]) (bool, error) {
	return profiles.Exists(ctx, id)
}
func RefreshProfileExpiry(ctx context.Context, profiles Profiles, id model.ID[mutatorqueries.Member], ttl cache.TTL) (bool, error) {
	return profiles.Expire(ctx, id, ttl)
}
func ForgetProfiles(ctx context.Context, profiles Profiles, ids ...model.ID[mutatorqueries.Member]) (uint64, error) {
	return profiles.ForgetMany(ctx, ids...)
}
