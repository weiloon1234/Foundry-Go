package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() {
	_ = expr.SubstringAt(expr.Value(scalars.Records.Name), expr.Value(scalars.Records.Name), expr.Value(scalars.Records.Name))
}
