package bool_projection

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type A struct{ Flag bool }

func bad(b rows.Builder[A], a *A) rows.Field[A, string] { return rows.BoolOf(b, &a.Flag) }
