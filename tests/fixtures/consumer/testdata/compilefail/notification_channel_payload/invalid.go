package compilefail

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(recipient notifying.UserRecipient, channel notifications.Channel[models.User, notifying.OrderCard]) {
	_ = notifications.Bind[models.User, model.ID[models.User], notifying.OrderPlaced](notifying.Placed, recipient, channel)
}
