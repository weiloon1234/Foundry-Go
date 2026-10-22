package projection_type

import (
	"foundry.test/goway/models"
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func F() {
	u := rows.As[struct{}](models.Users.Query(), "u")
	rows.Map(reporting.OrderRows.Amount, expr.At(rows.Scope(u), models.Users.Email))
}
