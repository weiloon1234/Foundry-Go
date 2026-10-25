package cursor_compile_promoted

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type wrapper struct{ rows.CursorQuery[models.User] }

var _, _ = rows.CompileCursor(wrapper{})
