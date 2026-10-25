package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type alias struct{}

var invalid = rows.As[alias](models.Users.Query().ForUpdate(), "locked")
