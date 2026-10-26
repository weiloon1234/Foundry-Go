package bool_row_value

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

type A struct{ Flag string }

func bad(b rows.Builder[A], a *A) { _ = rows.BoolOf(b, &a.Flag) }
