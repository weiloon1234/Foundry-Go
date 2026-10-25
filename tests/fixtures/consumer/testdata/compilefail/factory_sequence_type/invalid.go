package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func invalid() {
	_, _ = factory.Define[models.User, model.ID[models.User]](models.Users.Table, func(context.Context, string) ([]orm.Assignment[models.User], error) {
		return []orm.Assignment[models.User]{}, nil
	})
}
