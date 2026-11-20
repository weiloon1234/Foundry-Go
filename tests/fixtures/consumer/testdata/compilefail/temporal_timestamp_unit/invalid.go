package invalid

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	f := temporalqueries.Samples
	_ = expr.TruncateLocal(expr.Value(f.Local), expr.DateDay)
}
