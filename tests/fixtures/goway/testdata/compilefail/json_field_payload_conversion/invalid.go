package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() { _ = expr.JSONField[jsonpaths.Preferences, int](jsonpaths.Theme) }
