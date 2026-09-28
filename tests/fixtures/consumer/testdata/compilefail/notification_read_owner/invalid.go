package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(inbox notifications.Inbox[models.User, model.ID[models.User]], id notifications.ID[models.Order]) {
	_, _ = inbox.MarkRead(context.Background(), id)
}
