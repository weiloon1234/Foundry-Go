package raw_fragment_owner

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = models.Users.Query().Where(orm.WhereRaw[models.Order]("true"))
