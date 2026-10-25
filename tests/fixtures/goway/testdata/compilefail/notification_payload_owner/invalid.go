package compilefail

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

func invalid(binding notifications.Binding[models.User, model.ID[models.User], OrderPlaced], user models.User) {
	_, _ = binding.Capture(context.Background(), orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user), OrderCard{}, notifications.ID[models.User]{})
}

type OrderPlaced struct{ Summary string }
type OrderCard struct{ Summary string }
