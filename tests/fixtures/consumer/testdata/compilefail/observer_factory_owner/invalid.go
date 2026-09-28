package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
)

func wrongObserverFactory() {
	observer := lifecycle.NewObserver[models.User, models.UserHooks]("user.audit")
	_, _ = observer.Declare(func() models.GroupHooks { return models.GroupHooks{} })
}
