package invalid

import (
	"context"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

func invalid(store *extensions.Store, profile profiles.Profile) {
	_, _ = countries.Find(context.Background(), store, profile.ID)
}
