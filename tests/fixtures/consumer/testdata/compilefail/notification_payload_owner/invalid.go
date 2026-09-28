package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(binding notifying.OrdersBinding, user models.User) {
	_, _ = binding.Capture(context.Background(), user.FoundryReference(), notifying.OrderCard{}, notifications.ID[models.User]{})
}
