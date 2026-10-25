package invalid

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = orm.LoadFrom(context.Background(), nil, models.Users.Query(), []models.Order{})
var _, _ = orm.LoadMissingFrom(context.Background(), nil, models.Users.Query(), []models.Order{})
