package invalid

import (
	"context"
	"foundry.test/consumer/messaging"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(t messaging.MemberChanges, id model.ID[models.User]) {
	_, _ = t.Subscribe(context.Background(), id)
}
