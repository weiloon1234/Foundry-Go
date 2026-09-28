// Package configured proves framework-owned named-service assembly from an
// independent consumer module, without native drivers or infrastructure factories.
package configured

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

const Reports cache.StoreName = "reports"

//foundry:config
type Settings struct{ Services infrastructure.Settings }

func Defaults() Settings {
	s := infrastructure.DefaultSettings()
	s.Cache.Stores = infrastructure.CacheStores{"default": infrastructure.DefaultCacheSettings(), Reports: infrastructure.DefaultCacheSettings()}
	return Settings{Services: s}
}
func Build(ctx context.Context, settings Settings) (*foundation.App, error) {
	plan, err := infrastructure.Configure(settings.Services)
	if err != nil {
		return nil, err
	}
	return plan.Register(foundation.NewBuilder()).Build(ctx)
}

var Greeting = cache.Define("greetings", cache.StringKeys[string](), cache.JSON[string]())

// Greetings is ordinary constructor injection. Request handlers receive this
// concrete dependency, while context.Context carries cancellation/attribution.
func Greetings(configured *infrastructure.Services) (cache.Cache[string, string], error) {
	store, err := configured.Cache()
	if err != nil {
		return cache.Cache[string, string]{}, err
	}
	return Greeting.Bind(store)
}
func ReportsCache(configured *infrastructure.Services) (*cache.Store, error) {
	return configured.Caches.Store(Reports)
}
