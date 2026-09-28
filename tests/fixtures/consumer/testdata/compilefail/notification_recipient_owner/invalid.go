package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(binding notifying.OrdersBinding, other models.Order) {
	_, _ = binding.Capture(context.Background(), other.FoundryReference(), notifying.OrderPlaced{}, notifications.ID[models.User]{})
}
