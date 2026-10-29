package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type alias struct{}

var invalid = rows.As[alias](orm.Lock(models.Users.Query()), "locked")
