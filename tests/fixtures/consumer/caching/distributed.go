package caching

import (
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// DistributedStore borrows the application's lease manager and its Redis authority.
// RememberProfile, model IDs, getter selection and tag declarations stay unchanged.
func DistributedStore(manager *lease.Manager) (*cache.Store, error) {
	return cache.NewCoordinatedStore(manager, cache.DefaultConfig(manager.Namespace()), cache.DefaultCoordinationConfig())
}
func BindDistributed(manager *lease.Manager) (Profiles, error) {
	store, err := DistributedStore(manager)
	if err != nil {
		return Profiles{}, err
	}
	return Bind(store)
}
