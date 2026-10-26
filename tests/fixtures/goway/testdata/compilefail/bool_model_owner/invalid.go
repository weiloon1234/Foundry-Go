package bool_model_owner

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type A struct{ Flag bool }
type B struct{}

func bad(b orm.Builder[A], a *A) orm.Column[B, bool] { return orm.BoolOf(b, &a.Flag) }
