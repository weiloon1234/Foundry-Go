package invalid

import (
	"context"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(manager *translations.Manager, profile profiles.Profile, raw string) {
	_ = profiles.Label.Set(context.Background(), manager, profile.FoundryReference(), raw, "text")
}
