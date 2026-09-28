package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/metadata"
)

func invalid(manager *metadata.Manager, other models.Order) {
	_, _ = profiles.PreferencesKey.Get(context.Background(), manager, other.FoundryReference())
}
