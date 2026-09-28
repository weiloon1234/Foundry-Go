package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func bad() {
	_ = auth.DefineProvider("users", (models.User{}).FoundryReference(), func(context.Context, model.ID[models.Group]) (value.Optional[models.User], error) {
		return value.Optional[models.User]{}, nil
	}, func(context.Context, models.User) (bool, error) { return true, nil })
}
