package invalid

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = orm.LoadFrom(context.Background(), nil, orm.Lock(models.Users.Query()), []models.User{})
