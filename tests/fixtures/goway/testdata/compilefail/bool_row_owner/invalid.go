package bool_row_owner

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type A struct{ Flag bool }
type B struct{}

func bad(b rows.Builder[A], a *A) rows.Field[B, bool] { return rows.BoolOf(b, &a.Flag) }
