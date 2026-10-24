package invalid

import (
	"context"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/metadata"
)

func invalid(manager *metadata.Manager, profile profiles.Profile) {
	_ = profiles.PreferencesKey.Set(context.Background(), manager, profiles.ProfileIdentity.ReferenceOf(profile), "raw JSON")
}
