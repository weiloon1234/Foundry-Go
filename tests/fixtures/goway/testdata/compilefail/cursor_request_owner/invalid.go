package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = orm.CursorPaginate(nil, nil, models.Users.Query(), orm.CursorRequest[models.Order]{Size: 2})
