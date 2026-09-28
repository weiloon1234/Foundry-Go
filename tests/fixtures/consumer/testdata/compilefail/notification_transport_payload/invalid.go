package compilefail

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(transport notifications.Transport[notifying.OrderPlaced]) {
	_ = notifications.Custom[models.User, notifying.OrderPlaced, notifying.OrderCard]("custom", notifying.OrderCardJSON(), nil, transport)
}
