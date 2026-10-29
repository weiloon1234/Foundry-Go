package aggregate_input_bytes

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type input struct{ Text string }

var _ = expr.Min(orm.InputColumnExpr(orm.InputColumn[models.User, input, []byte]{}))
