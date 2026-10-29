package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ex database.Executor) { _, _ = orm.AllLocked(nil, ex, orm.Lock(models.Users.Query())) }
