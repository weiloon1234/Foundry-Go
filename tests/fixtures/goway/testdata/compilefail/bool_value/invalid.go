package bool_value

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type A struct{ Flag string }

func bad(b orm.Builder[A], a *A) { _ = orm.BoolOf(b, &a.Flag) }
