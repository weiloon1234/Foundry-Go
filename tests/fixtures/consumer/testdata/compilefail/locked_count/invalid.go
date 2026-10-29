package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

var _, _ = orm.Count(nil, nil, orm.Lock(models.Users.Query()))
