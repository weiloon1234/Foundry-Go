package bool_row_phase

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type A struct{ Flag bool }

func bad(b rows.Builder[A], a *A) expr.Selected[A, bool] { return rows.BoolOf(b, &a.Flag) }
