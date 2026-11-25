package configured

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/settings"
)

func cachedSiteName(store *extensions.Store) (*settings.Manager, error) {
	// doc-example:begin cache-site-name
	manager, err := settings.New(store,
		SiteName.RegistrationWith(settings.Options[string]{Cache: 30 * time.Second}))
	// doc-example:end cache-site-name
	return manager, err
}
