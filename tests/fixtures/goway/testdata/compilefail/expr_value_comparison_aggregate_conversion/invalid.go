package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type scope struct{}

func escape(source expr.ValueComparison[scope, int64]) { _ = expr.Aggregate[scope, int64](source) }
