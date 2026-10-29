package cursor_compile_transaction

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _, _ = rows.CompileCursor(rows.LockedRecord(orm.Lock(models.Users.Query())))
