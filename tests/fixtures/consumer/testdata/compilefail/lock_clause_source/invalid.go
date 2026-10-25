package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type alias struct{}

var q = models.Users.Query()
var invalid = rows.AsRecord[alias, models.User](rows.LockRows(rows.SelectRecord(rows.From(q), rows.ModelScope(q)), rows.UpdateLock(rows.Target(rows.ModelScope(q)))), "locked")
