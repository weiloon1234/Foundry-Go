package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(binding notifying.OrdersBinding, user models.User) {
	_, _ = binding.Capture(context.Background(), orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user), notifying.OrderCard{}, notifications.ID[models.User]{})
}
