package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid, _ = orm.CursorPaginate(context.Background(), nil, models.Users.Query(), orm.CursorRequest[models.Order]{Size: 2})
