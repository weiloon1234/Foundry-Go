package developeroperations

import (
	"foundry.test/consumer/caching"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
)

// doc-example:begin named-cache
func NamedProfiles(services application.Services, name cache.StoreName) (caching.Profiles, error) {
	store, err := services.Caches.Store(name)
	if err != nil {
		return caching.Profiles{}, err
	}
	return caching.Bind(store)
}

// doc-example:end named-cache
