package invalid

import (
	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type alias struct{}

var source = rows.As[alias](inputqueries.Members.Query(), "other")
var _ = orm.ConflictSet(expr.At(rows.Scope(source), inputqueries.Members.Email), inputqueries.EmailInput{})
