package cursor_compile_transaction

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _, _ = rows.CompileCursor(rows.LockedRecord(models.Users.Query().ForUpdate()))
