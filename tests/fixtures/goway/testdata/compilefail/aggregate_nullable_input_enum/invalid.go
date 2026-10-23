package aggregate_nullable_input_enum

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type input struct{ Text string }

var _ = expr.Min(orm.NullableInputEnum[models.User, input, models.Level]{})
