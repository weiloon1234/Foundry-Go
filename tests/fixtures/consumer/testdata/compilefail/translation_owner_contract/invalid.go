package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(manager *translations.Manager, other models.Order) {
	_ = profiles.Label.Set(context.Background(), manager, other.FoundryReference(), "en", "text")
}
