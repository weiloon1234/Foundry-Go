package aggregate_nullable_input_json

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type input struct{ Text string }

var _ = expr.Min(orm.NullableInputJSONExpr(orm.NullableInputJSON[models.User, input, string]{}))
