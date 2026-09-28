package caching

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cache"
)

// InvalidateSnapshots clears this application's cache namespace across instances.
// Handles remain bound; later reads/loaders preserve their concrete model/DTO types.
func InvalidateSnapshots(ctx context.Context, store *cache.Store) error {
	return store.Invalidate(ctx)
}
