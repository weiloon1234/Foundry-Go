package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(binding notifying.OrdersBinding, other models.Order) {
	_, _ = binding.Capture(context.Background(), orm.IdentityOf(models.Orders.Table, models.Orders.Key()).ReferenceOf(other), notifying.OrderPlaced{}, notifications.ID[models.User]{})
}
