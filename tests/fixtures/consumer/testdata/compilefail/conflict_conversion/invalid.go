package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Conflict[models.Order](orm.OnConflict[models.User]().DoNothing())
