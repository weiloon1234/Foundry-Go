package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type other struct{}

var alias = rows.As[other](scalars.Records.Query(), "other")
var _ = expr.Where(scalars.Records.Query(), expr.TrigramMatch(expr.At(rows.Scope(alias), orm.TextExpr(scalars.Records.Name)), "word"))
