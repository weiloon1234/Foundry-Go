package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func wrongObserverConstructor(r *foundation.Registrar, pool foundation.Key[*database.DB]) {
	observer := lifecycle.NewObserver[models.User, models.UserHooks]("user.audit")
	_ = database.RegisterObserver(r, pool, observer, func(foundation.Resolver) (func() models.GroupHooks, error) {
		return func() models.GroupHooks { return models.GroupHooks{} }, nil
	})
}
