package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _, _ = orm.CursorPaginate(nil, nil, models.Users.Query(), query.CursorRequest[models.Order]{Size: 2})
