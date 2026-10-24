package invalid

import (
	"foundry.test/goway/models/jsonpaths"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var _ = expr.JSONFieldOf(func(p *jsonpaths.Preferences) *string { return &p.Typo })
