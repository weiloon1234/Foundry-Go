package json_sum

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type doc struct{}

var _ = expr.Sum(orm.JSON[doc, string]{})
