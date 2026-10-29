package invalid

import (
	"foundry.test/goway/coremodels"

	"foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type alias struct{}

var source = rows.As[alias](mutators.Members.Query(), "other")
var _ = orm.ConflictSet(expr.At(rows.Scope(source), orm.InputTextExpr(mutators.Members.Email)), coremodels.EmailInput{})
