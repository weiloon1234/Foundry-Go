package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "foundry.test/consumer/models"

var invalid = orm.TextEq(models.Users.Email, 42)
