package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/notifying"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(record notifications.Record[models.Order]) {
	_, _ = notifying.Database.Decode(context.Background(), notifying.Placed, record)
}
