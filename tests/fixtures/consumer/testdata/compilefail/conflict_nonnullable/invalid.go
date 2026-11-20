package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.SetNull(models.Users.Email)
