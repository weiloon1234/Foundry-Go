package aggregate_input_text_sum

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type input struct{ Text string }

var _ = expr.Sum(orm.InputText[models.User, input, string]{})
